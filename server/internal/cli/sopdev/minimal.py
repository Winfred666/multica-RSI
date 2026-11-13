from sop import FINISHED, Option, State, Workflow

workflow = Workflow(
    State('ready', 'Choose finish to complete this procedure.', initial=True,
          options=(Option('finish', FINISHED, 'Complete the procedure'),)),
)
