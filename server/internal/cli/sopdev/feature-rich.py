from pathlib import Path
from sop import Branch, FINISHED, Option, Parameter, State, Workflow


def inspect_file(args, context):
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
    Option('check', 'Inspect the file', inspect_file,
           params=(Parameter('--path', 'File to inspect'),)),
    State('inspect', 'Choose a file to inspect. Relative paths use the caller directory.', initial=True),
    Branch('route', 'Route by file size'),
    Option('accept', 'Accept and complete'),
    Option('retry', 'Choose another file'),
    State('review', 'Review the file.', view=review,
          preview='Review {path} ({size} bytes), then accept or retry.'),
    edges=(
        ('inspect', 'check'), ('check', 'route'),
        ('route', 'review'), ('route', 'inspect'),
        ('review', 'accept'), ('accept', FINISHED),
        ('review', 'retry'), ('retry', 'inspect'),
    ),
)
