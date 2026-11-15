from sop import FINISHED, Option, State, Workflow

workflow = Workflow(
    Option('finish', 'Complete the procedure'),
    State('ready', 'Choose finish to complete this procedure.', initial=True),
    edges=(('ready', 'finish'), ('finish', FINISHED)),
)
