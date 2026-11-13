from pathlib import Path
from sop import Branch, FINISHED, Option, Parameter, State, Workflow


def describe(context):
    return {'instruction': 'Choose a file to inspect. Relative paths use the caller directory.'}


def inspect_file(args, context):
    """```dot
    digraph inspect {
        "Read file size" -> "Review" [label="nonempty"];
        "Read file size" -> "Choose another file" [label="empty"];
    }
    ```"""
    path = Path(args.path)
    if not path.is_file():
        raise context['error']('missing_file', 'Choose an existing file.')
    size = path.stat().st_size
    context['state'].update(path=str(path.resolve()), size=size)
    return {'phase': 'review' if size else 'inspect'}


def review(context):
    state = context['state']
    return {'instruction': f"Review {state['path']} ({state['size']} bytes), then accept or retry."}


workflow = Workflow(
    State('inspect', 'Choose a file.', initial=True, view=describe, options=(
        Option('check', 'route', 'Inspect the file', inspect_file,
               params=(Parameter('--path', 'File to inspect'),)),
    )),
    Branch('route', 'Route by file size', {'nonempty': 'review', 'empty': 'inspect'}),
    State('review', 'Review the file.', view=review, options=(
        Option('accept', FINISHED, 'Accept and complete'),
        Option('retry', 'inspect', 'Choose another file'),
    )),
)
