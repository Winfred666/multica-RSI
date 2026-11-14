"""Small state/option model used by the standalone wrapper."""
import argparse
import ast
import inspect
import os
import re
import textwrap

FINISHED = 'FINISHED'


class Parameter:
    def __init__(self, flag, prompt='', *, required=True, value=None, **kwargs):
        self.flag, self.prompt = flag, prompt
        self.required, self.value, self.kwargs = required, value, kwargs

    def add_to(self, parser):
        parser.add_argument(self.flag, help=self.prompt, **self.kwargs)

    def words(self):
        if self.value is not None:
            return [self.flag, str(self.value)]
        if not self.required:
            return []
        placeholder = '<' + self.flag.lstrip('-').replace('-', '_') + '>'
        return [self.flag, placeholder] if self.flag.startswith('-') else ['--', placeholder]


class Option:
    def __init__(self, name, target, prompt, action=None, *, params=()):
        self.name, self.target, self.prompt = name, target, prompt
        self.action, self.params = action, tuple(params)

    def callback(self):
        return [self.name, *(word for param in self.params for word in param.words())]

    def diagram(self):
        doc = inspect.getdoc(self.action) if self.action else ''
        match = re.search(r'```dot\s*\n(.*?)```', doc or '', re.S)
        return match.group(1).strip() if match else ''


class State:
    def __init__(self, name, description, *, initial=False, final=False,
                 options=(), precheck=None, view=None, owner=None):
        self.name, self.description, self.owner = name, description, owner
        self.initial, self.final = initial, final
        self.options, self.precheck, self.view = options, precheck, view

    def choices(self, context):
        options = tuple(self.options(context) if callable(self.options) else self.options)
        context.setdefault('resolved_options', {})[self.name] = options
        return options


class Completed(Exception):
    """A read-only precheck found that this issue has already completed the SOP."""
    def __init__(self, instruction, data=None, *, target='finished'):
        self.result = {'phase': target, 'instruction': instruction, 'data': data or {}}


class Branch:
    """An automatic transition selected by a callback, or the preceding action phase."""
    def __init__(self, name, description, routes, *, select=None):
        self.name, self.description, self.routes = name, description, dict(routes)
        self.select = select


class Workflow:
    FINISHED = FINISHED

    def __init__(self, *nodes, params=(), handoff=None):
        self.states = {node.name: node for node in nodes if isinstance(node, State)}
        self.branches = {node.name: node for node in nodes if isinstance(node, Branch)}
        self.options = {node.name: node for node in nodes if isinstance(node, Option)}
        self.params, self.handoff = tuple(params), handoff
        states = tuple(self.states.values())
        initial = [state for state in states if state.initial]
        if (len(set(self.states) | set(self.branches) | set(self.options)) != len(nodes) or
                len(initial) != 1 or initial[0].final):
            raise ValueError('Declare unique states and one initial state')
        self.initial = initial[0].name
        self.max_action_depth = int(os.environ.get('MULTICA_SOP_MAX_ACTION_DEPTH', '5'))
        if self.max_action_depth < 1:
            raise ValueError('SoP maximum action depth must be positive')
        for node in nodes:
            if not re.fullmatch(r'[a-z][a-z0-9_-]*' if isinstance(node, Option) else r'[a-z][a-z0-9_]*', node.name) or node.name in {'init', 'repaired', 'error', 'blocked'}:
                raise ValueError('Invalid or reserved node name: ' + node.name)
        for branch in self.branches.values():
            if not branch.routes or any(target not in self.states.keys() | self.branches.keys() | self.options.keys() | {FINISHED} for target in branch.routes.values()):
                raise ValueError('Branch routes must name declared nodes or FINISHED: ' + branch.name)
        for state in states:
            if state.final and (callable(state.options) or state.options):
                raise ValueError('Final states cannot have options')
            if not callable(state.options):
                self.validate_options(self.choices(state, {}))
        self.validate_options(self.options.values())
        self.validate_graph()

    def validate_options(self, options):
        for option in options:
            if option.target not in self.states.keys() | self.branches.keys() | self.options.keys() | {FINISHED} or not re.fullmatch(r'[a-z][a-z0-9_-]*', option.name) or option.name in {'init', 'repaired', 'quit'}:
                raise ValueError('Option needs a command name and a declared target: ' + option.name)

    def choices(self, state, context):
        options = tuple(self.options[option] if isinstance(option, str) else option
                        for option in state.choices(context))
        for option in options:
            if option.name in self.options and option is not self.options[option.name]:
                raise ValueError('Reuse the registered option: ' + option.name)
        context.setdefault('resolved_options', {})[state.name] = options
        return options

    def validate_graph(self, resolved=None, *, reply_guidance=False):
        """Every automatic node needs an exit; State-to-State cycles are allowed."""
        edges = {name: tuple(branch.routes.values()) for name, branch in self.branches.items()}
        edges.update({name: (option.target,) for name, option in self.options.items()})
        reachable = set(self.states) | {FINISHED}
        while exits := {name for name, targets in edges.items() if any(target in reachable for target in targets)} - reachable:
            reachable.update(exits)
        stranded = edges.keys() - reachable
        if stranded:
            raise ValueError('Automatic nodes must reach a State or FINISHED: ' + ', '.join(sorted(stranded)))

        if reply_guidance:
            def check(target, action, visited):
                key = (target, action)
                if key in visited:
                    return
                visited = visited | {key}
                if target in self.options:
                    option = self.options[target]
                    check(option.target, option.action, visited)
                elif target in self.branches:
                    for destination in edges[target]:
                        check(destination, action, visited)
                elif target in self.states and isinstance(self.states[target].owner, dict) and 'issue_id' in self.states[target].owner:
                    tree = ast.parse(textwrap.dedent(inspect.getsource(action))) if action else ast.parse('')
                    returns = (node.value if isinstance(node, ast.Return) else node.body
                               for node in ast.walk(tree) if isinstance(node, (ast.Return, ast.Lambda)))
                    if not any(isinstance(value, ast.Dict) and any(
                            isinstance(key, ast.Constant) and key.value == 'this_round_reply_guidance'
                            for key in value.keys) for value in returns):
                        raise ValueError('Final action into addressed state ' + target +
                                         ' must return this_round_reply_guidance')
            for state in self.states.values():
                if state.owner and not (isinstance(state.owner, dict) and 'issue_id' in state.owner):
                    for option in self.choices(state, {}) if not callable(state.options) else ():
                        check(option.target, option.action, set())

    def is_finished(self, phase):
        return phase == FINISHED or self.states[phase].final

    def configure(self, parser):
        for param in self.params:
            param.add_to(parser)

    def before(self, context):
        state = self.states[context['phase']]
        context['initial'] = state.precheck(context) if state.precheck else None
        return context['initial']

    def after(self, context, args, initial, result):
        context.update(args=args, initial=initial, result=result)
        if result['phase'] == FINISHED:
            return
        state = self.states[result['phase']]
        if state.precheck:
            state.precheck(context)

    def guidance(self, state, context, result=None):
        context['phase'] = state.name
        result = dict(result or {})
        view = state.view(context) if state.view else {}
        options = self.choices(state, context)
        self.validate_options(options)
        context.setdefault('option_diagrams', {})[state.name] = {option.name: option.diagram() for option in options}
        context.setdefault('option_targets', {})[state.name] = {option.name: option.target for option in options}
        text = result.pop('instruction', None) or view.get('instruction') or state.description
        result.setdefault('data', view.get('data'))
        return {**result, 'phase': state.name, 'instruction': text,
                'callback': options[0].callback() if options else None,
                'callback_instruction': options[0].prompt if options else None,
                'alternatives': [{'instruction': option.prompt, 'callback': option.callback()}
                                 for option in options[1:]]}

    def step(self, args, context, phase):
        state = self.states[phase]
        context.update(phase=phase, args=args)
        if args.event == 'init':
            if args.extra:
                raise context['error']('invalid_input', 'Choose an option before providing its parameters')
            return self.guidance(state, context)
        option = self.select_option(args, context, state)
        result = self.execute_option(option, args, context)
        target = result.get('phase', state.name) if result.get('accepted', True) else state.name
        if target == FINISHED:
            return {**result, 'phase': FINISHED, 'instruction': ''}
        return self.guidance(self.states[target], context, {**result, 'phase': target})

    def select_option(self, args, context, state):
        options = self.choices(state, context)
        self.validate_options(options)
        context.setdefault('option_diagrams', {})[state.name] = {option.name: option.diagram() for option in options}
        context.setdefault('option_targets', {})[state.name] = {option.name: option.target for option in options}
        matches = [option for option in options if option.name == args.event]
        if not matches:
            raise context['error']('invalid_transition', 'Choose one of the returned options for ' + state.name)
        parsed, errors = [], []
        for option in matches:
            parser = argparse.ArgumentParser(add_help=False, allow_abbrev=False, exit_on_error=False)
            for param in option.params:
                param.add_to(parser)
            try:
                values, unknown = parser.parse_known_args(args.extra)
                if unknown:
                    raise ValueError('Unknown option parameters: ' + ' '.join(unknown))
                for param in option.params:
                    name = param.flag.lstrip('-').replace('-', '_')
                    value = getattr(values, name)
                    if param.required and (value is None or value == [] or isinstance(value, str) and not value.strip()):
                        raise ValueError(param.flag + ' is required: ' + param.prompt)
                    if param.value is not None and str(value) != str(param.value):
                        raise ValueError('Use the returned ' + param.flag + ' value')
                parsed.append((option, values))
            except (argparse.ArgumentError, ValueError) as error:
                errors.append(str(error))
        if len(parsed) != 1:
            raise context['error']('invalid_input', errors[0] if errors else 'Choose a single returned option')
        option, values = parsed[0]
        vars(args).update(vars(values))
        return option

    def bind_inputs(self, option, args, context):
        for param in option.params:
            name = param.kwargs.get('dest', param.flag.lstrip('-').replace('-', '_'))
            value = getattr(args, name, param.kwargs.get('default'))
            if value is None and param.value is not None:
                value = param.value
            if value is not None and param.kwargs.get('type'):
                convert = param.kwargs['type']
                try:
                    value = [convert(item) for item in value] if isinstance(value, list) else convert(value)
                except (TypeError, ValueError) as error:
                    raise context['error']('invalid_action_input', str(error)) from error
            if param.required and (value is None or value == [] or isinstance(value, str) and not value.strip()):
                raise context['error']('invalid_action_input', param.flag + ' must be bound before action ' + option.name)
            if param.value is not None and str(value) != str(param.value):
                raise context['error']('invalid_action_input', 'Use the returned ' + param.flag + ' value')
            if value is not None and 'choices' in param.kwargs:
                values = value if isinstance(value, list) else [value]
                if any(item not in param.kwargs['choices'] for item in values):
                    raise context['error']('invalid_action_input', 'Invalid ' + param.flag + ' for action ' + option.name)
            setattr(args, name, value)

    def resolve_target(self, option, result, args, context):
        target = option.target
        if target not in self.branches:
            if result.get('phase', target) != target:
                raise context['error']('invalid_action_output', 'Action must use its declared target: ' + option.name)
            return target
        visited = set()
        while target in self.branches:
            if target in visited:
                raise context['error']('invalid_branch_output', 'Automatic branch loop: ' + target)
            visited.add(target)
            branch = self.branches[target]
            if branch.select:
                route = branch.select(args, context)
                if route not in branch.routes:
                    raise context['error']('invalid_branch_output', 'Return a declared route for branch ' + branch.name)
                target = branch.routes[route]
            else:
                target = result.get('phase')
                if target not in branch.routes.values():
                    raise context['error']('invalid_branch_output', 'Return a declared phase for branch ' + branch.name)
        return target

    def execute_option(self, option, args, context, depth=1):
        if depth > self.max_action_depth:
            raise context['error']('action_depth_exceeded',
                                   'SoP action depth exceeds ' + str(self.max_action_depth))
        if depth > 1:
            self.bind_inputs(option, args, context)
        result = option.action(args, context) if option.action else {}
        result = result or {}
        if not result.get('accepted', True):
            return result
        target = self.resolve_target(option, result, args, context)
        if target in self.options:
            return {**result, **self.execute_option(self.options[target], args, context, depth + 1)}
        return {**result, 'phase': target}
