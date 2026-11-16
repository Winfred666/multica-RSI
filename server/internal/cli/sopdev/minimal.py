from sop import FINISHED, Option, State, Workflow

workflow = Workflow(
    State('ready', 'Choose finish to complete the procedure and end the turn silently.', initial=True),
    Option('finish', 'Complete the procedure'),
    edges=(('ready', 'finish'), ('finish', FINISHED)),
)
