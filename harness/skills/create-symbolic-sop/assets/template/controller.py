"""Refine one bound workspace; parallel issues each own their task record."""
import importlib.util
import math
from pathlib import Path
import re
import sys

import monitor
from sop import Branch, Option, Parameter, State, Workflow


ROOT = Path(__file__).resolve().parent


def save(context, **values):
    context['state'].update(values)
    monitor.write(ROOT / '.state' / (context['task_id'] + '.json'), context['state'])


def benchmark(context):
    path = context['state']['test_script']
    spec = importlib.util.spec_from_file_location('refinement_test', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    for name in ('validate', 'score', 'is_complete'):
        if not callable(getattr(module, name, None)):
            raise ValueError('Test script requires ' + name)
    return module


def valid(context):
    result = benchmark(context).validate()
    if not isinstance(result, bool):
        raise ValueError('validate() must return a boolean')
    return result


def configure(args, context):
    """```mermaid
    flowchart LR
        configure_bind["Bind script and workspace"] --> configure_validate["Run delivery-format check"]
        configure_validate --> configure_save["Save baseline_pending and return phase"]
    ```"""
    save(context, test_script=str(args.test_script.resolve()), workspace=str(Path.cwd()))
    baseline = valid(context)
    save(context, baseline_pending=not baseline)
    return {'phase': 'variant' if baseline else 'scaffold'}


def precheck(context):
    state = context['state']
    if not Path(state['test_script']).is_file() or not Path(state['workspace']).is_dir():
        raise AssertionError('The bound workspace and test script must exist.')
    phase = context.get('result', {}).get('phase', context['phase'])
    if phase in {'variant', 'finished'} and not valid(context):
        raise AssertionError('The baseline must pass the test script’s delivery-format gate.')
    return {'test_script': state['test_script'], 'workspace': state['workspace']}


def view(context):
    state = context['state']
    return {'data': {key: state.get(key) for key in ('test_script', 'description', 'test_result')}}


def test(args, context):
    """```mermaid
    flowchart LR
        test_gate{"One paragraph and valid delivery?"}
        test_gate -->|yes| test_launch["Start monitor and return job handle"]
        test_gate -->|no| test_reject["Reject; keep current state"]
    ```"""
    description = args.description.strip()
    if not description or re.search(r'\n\s*\n', description):
        return {'accepted': False, 'instruction': 'Describe the idea in one paragraph.'}
    if not valid(context):
        return {'accepted': False, 'instruction': 'Implement a template in the testable delivery format before testing.'}
    state = context['state']
    job = monitor.start([sys.executable, state['test_script']], state['workspace'], Path(state['workspace']) / 'logs')
    save(context, description=description, job=job)
    return {'phase': 'test', 'data': {'job': job}}


def observe(args, context):
    """```mermaid
    flowchart LR
        observe_wait["Collect new logs or process exit"] --> observe_parse["Read completed delivery and score; capture errors"]
        observe_parse --> observe_save["Save report and baseline when applicable"]
        observe_save --> observe_return["Return report and phase"]
    ```"""
    state = context['state']
    report = monitor.wait(state['job'], state.setdefault('monitor', {}))
    if report['state'] == 'running':
        save(context, test_result=report)
        return {'phase': 'test', 'data': report}
    delivery = None
    if report['returncode'] == 0 and not report.get('exception'):
        try:
            delivery = monitor.delivery(state['job'])
            score = benchmark(context).score(delivery)
            if isinstance(score, bool) or not isinstance(score, (int, float)) or not math.isfinite(score):
                raise ValueError('The test must record a finite score.')
            if not isinstance(benchmark(context).is_complete(delivery), bool):
                raise ValueError('is_complete(delivery) must return a boolean.')
        except (ValueError, KeyError, TypeError) as error:
            report['error'] = str(error)
            delivery = None
    report['delivery'] = delivery
    save(context, test_result=report)
    if state['baseline_pending'] and delivery is not None and valid(context):
        save(context, baseline_pending=False)
        monitor.write(Path(state['job']) / 'baseline.json', {'description': state['description'], 'delivery': delivery})
        return {'phase': 'finished' if benchmark(context).is_complete(delivery) else 'variant', 'data': report}
    return {'phase': 'review', 'data': report}


def exception(args, context):
    """```mermaid
    flowchart LR
        exception_stop["Stop monitor"] --> exception_observe["Collect and route result via observe"]
    ```"""
    monitor.stop(context['state']['job'])
    return observe(args, context)


def record(args, context):
    """```mermaid
    flowchart LR
        record_review{"Review is one paragraph?"}
        record_review -->|no| record_reject["Reject; keep current state"]
        record_review -->|yes| record_save["Write review.json"]
        record_save --> record_return["Return report and phase"]
    ```"""
    review = args.review.strip()
    if not review or re.search(r'\n\s*\n', review):
        return {'accepted': False, 'instruction': 'Review this result in one paragraph.'}
    state = context['state']
    report = state['test_result']
    monitor.write(Path(state['job']) / 'review.json', {'description': state['description'], 'review': review, **report})
    delivery = report.get('delivery')
    return {'phase': 'finished' if delivery is not None and benchmark(context).is_complete(delivery) else
            'scaffold' if state['baseline_pending'] else 'variant', 'data': report}


workflow = Workflow(
    State('initialize', 'Bind the workspace’s test and result-recording script.', initial=True, options=(
        Option('configure', 'configure_result', 'Bind the supplied test script; its delivery gate selects scaffold or variant.', configure,
               params=(Parameter('--test-script', 'Absolute Python test script path', type=Path),)),)),
    Branch('configure_result', 'Delivery format valid?', {'yes': 'variant', 'no': 'scaffold'}),
    State('scaffold', 'Implement a minimal template baseline in the testable delivery format. Describe it in one paragraph.',
          precheck=precheck, view=view, options=(
              Option('test', 'test', 'Test the template; a scorable baseline enters variant without a review.', test,
                     params=(Parameter('--description', 'One paragraph describing the template baseline'),)),)),
    State('variant', 'Improve the bound solution. Describe the idea in one paragraph before testing.',
          precheck=precheck, view=view, options=(
              Option('test', 'test', 'Test the bound solution.', test,
                     params=(Parameter('--description', 'One paragraph describing this idea'),)),)),
    State('test', 'Read the collected progress and choose continue or exception in this turn.', precheck=precheck, view=view, options=(
        Option('continue', 'test_result', 'Monitor until an important log, time checkpoint or test exit.', observe),
        Option('exception', 'test_result', 'End this experiment and review the recorded result.', exception),)),
    Branch('test_result', 'Test output', {'running': 'test', 'variant result': 'review',
                                       'baseline accepted': 'variant', 'baseline complete': 'finished'}),
    State('review', 'Review the recorded test result in one paragraph.', precheck=precheck, view=view, options=(
        Option('record', 'review_result', 'Record the review; the measured exit condition selects completion or another variant.', record,
               params=(Parameter('--review', 'One paragraph reviewing the result'),)),)),
    Branch('review_result', 'Review output', {'complete': 'finished', 'baseline pending': 'scaffold', 'refine': 'variant'}),
    State('finished', 'The measured delivery meets the test script’s exit condition. Deliver the result.', final=True, precheck=precheck),
)
