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
    def __init__(self, name, prompt='', action=None, *, params=()):
        self.name, self.prompt = name, prompt
        self.action, self.params = action, tuple(params)

    def callback(self):
        return [self.name, *(word for param in self.params for word in param.words())]

    def diagram(self):
        doc = inspect.getdoc(self.action) if self.action else ''
        match = re.search(r'```dot\s*\n(.*?)```', doc or '', re.S)
        return match.group(1).strip() if match else ''


class State:
    params = ()

    def __init__(self, name, description, *, initial=False, final=False,
                 options=None, precheck=None, view=None, owner=None):
        self.name, self.description, self.prompt, self.owner = name, description, description, owner
        self.initial, self.final = initial, final
        self.options, self.precheck, self.view = options, precheck, view

    def callback(self):
        return [self.name.lower()]


class Completed(Exception):
    """A read-only precheck found that this issue has already completed the SOP."""
    def __init__(self, instruction, data=None, *, target='finished'):
        self.result = {'phase': target, 'instruction': instruction, 'data': data or {}}


class Branch:
    """Select an outgoing destination, or use the preceding action's phase."""
    params = ()
    callback = State.callback

    def __init__(self, name, description, *, select=None):
        self.name, self.description, self.prompt = name, description, description
        self.select = select


class Workflow:
    FINISHED = FINISHED

    def __init__(self, *nodes, edges=(), params=(), handoff=None):
        self.states = {node.name: node for node in nodes if isinstance(node, State)}
        self.branches = {node.name: node for node in nodes if isinstance(node, Branch)}
        self.options = {node.name: node for node in nodes if isinstance(node, Option)}
        self.nodes = {node.name: node for node in nodes}
        self.nodes[FINISHED] = State(FINISHED, 'Finish.', final=True)
        self.edges = tuple(dict.fromkeys(tuple(edge) for edge in edges))
        self.adjacency = {name: [] for name in self.nodes}
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
            if not re.fullmatch(r'[a-z][a-z0-9_-]*' if isinstance(node, Option) else r'[a-z][a-z0-9_]*', node.name) or node.name in {'init', 'repaired', 'error', 'blocked'} or isinstance(node, Option) and node.name == 'quit':
                raise ValueError('Invalid or reserved node name: ' + node.name)
        for edge in self.edges:
            if len(edge) != 2:
                raise ValueError('Edges must be (source, destination) pairs: ' + repr(edge))
            source, destination = edge
            if source not in self.nodes or destination not in self.nodes:
                raise ValueError('Edge needs declared nodes: ' + source + ' -> ' + destination)
            self.adjacency[source].append(destination)
        for state in states:
            if state.options is not None and not callable(state.options):
                raise ValueError('State choices come from edges; options must be a runtime factory')
            if state.final and (state.options or self.adjacency[state.name]):
                raise ValueError('Final states cannot have options')
        self.validate_graph()

    def choices(self, state, context):
        options = state.options(context) if state.options else self.adjacency[state.name]
        options = tuple(options)
        if any((option if isinstance(option, str) else option.name) not in self.adjacency[state.name] for option in options):
            raise ValueError('Choose a declared destination for ' + state.name)
        options = tuple(self.nodes[option] if isinstance(option, str) else option for option in options)
        context.setdefault('resolved_options', {})[state.name] = options
        return options

    def graph(self):
        return self.nodes, self.edges

    def validate_graph(self, *, reply_guidance=False):
        """Every action path must reach a State or FINISHED; States end the walk."""
        nodes, adjacency = self.nodes, self.adjacency
        checked = set()

        def visit(name, path, action=None, require_reply=False):
            node = nodes.get(name)
            if name == FINISHED or isinstance(node, State):
                if (reply_guidance and require_reply and isinstance(node, State) and
                        isinstance(node.owner, dict) and 'issue_id' in node.owner):
                    tree = ast.parse(textwrap.dedent(inspect.getsource(action))) if action else ast.parse('')
                    returns = (item.value if isinstance(item, ast.Return) else item.body
                               for item in ast.walk(tree) if isinstance(item, (ast.Return, ast.Lambda)))
                    if not any(isinstance(value, ast.Dict) and any(
                            isinstance(key, ast.Constant) and key.value == 'this_round_reply_guidance'
                            for key in value.keys) for value in returns):
                        raise ValueError('Final action into addressed state ' + name +
                                         ' must return this_round_reply_guidance: ' + ' -> '.join(map(str, (*path, name))))
                return
            if name in path:
                raise ValueError('Action-only cycle: ' + ' -> '.join(map(str, (*path, name))))
            if node is None or not adjacency[name]:
                raise ValueError('Action path must reach a State or FINISHED: ' + ' -> '.join(map(str, (*path, name))))
            if isinstance(node, Option):
                action = node.action
            key = (name, id(action), require_reply)
            if key in checked:
                return
            for destination in adjacency[name]:
                visit(destination, (*path, name), action, require_reply)
            checked.add(key)

        for state in self.states.values():
            require_reply = bool(state.owner) and not (isinstance(state.owner, dict) and 'issue_id' in state.owner)
            for destination in adjacency[state.name]:
                visit(destination, (state.name,), require_reply=require_reply)
        for name, node in nodes.items():
            if not isinstance(node, State):
                visit(name, ())

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
        result = self.execute_node(option, args, context, parsed=True)
        target = result.get('phase', state.name) if result.get('accepted', True) else state.name
        if target == FINISHED:
            return {**result, 'phase': FINISHED, 'instruction': ''}
        return self.guidance(self.states[target], context, {**result, 'phase': target})

    def select_option(self, args, context, state):
        options = self.choices(state, context)
        matches = [option for option in options if option.name.lower() == args.event]
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

    def execute_node(self, node, args, context, depth=0, result=None, *, parsed=False):
        if isinstance(node, State):
            return {**(result or {}), 'phase': node.name}
        if isinstance(node, Option):
            depth += 1
            if depth > self.max_action_depth:
                raise context['error']('action_depth_exceeded',
                                       'SoP action depth exceeds ' + str(self.max_action_depth))
            if not parsed:
                self.bind_inputs(node, args, context)
            result = (node.action(args, context) if node.action else {}) or {}
            if not result.get('accepted', True):
                return result
        result = result or {}
        destinations = self.adjacency[node.name]
        branch = isinstance(node, Branch)
        target = (node.select(args, context) if branch and node.select else result.get('phase'))
        if not branch and len(destinations) == 1:
            target = destinations[0]
            if target not in self.branches and result.get('phase', target) != target:
                raise context['error']('invalid_action_output', 'Action must use a declared destination: ' + node.name)
        if target not in destinations:
            raise context['error']('invalid_branch_output' if branch else 'invalid_action_output',
                                   'Return a declared destination for ' + node.name)
        return {**result, **self.execute_node(self.nodes[target], args, context, depth, result)}
