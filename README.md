# Multica-RSI

We maintain this fork of [Multica](https://github.com/multica-ai/multica) for
experiments with agent workflows and SOP training. Read the
[upstream README](https://github.com/multica-ai/multica#readme) for the product
introduction, supported providers and installation options.

## Backend

- **Editable startup prompts.** We replaced embedded workflow instructions with
  workspace Go templates and SOP skills. You can edit the workspace prompt;
  the server validates the template on save.
- **Configurable subscriptions and wakeups.** You can control inherited sub-issue
  following per user and workspace. We retired the built-in child-done wakeup
  and moved parent wakeup policy into plugins. You can create explicit waits
  through the existing wakeup API.
- **Hosted plugins with PAT credentials.** Workspace owners and admins can supply
  a personal access token for packaged hooks that execute on the server. We
  added encrypted credential storage and execution controls. Hooks run as the
  PAT owner with unsandboxed host access.
- **Concurrency.** We raised the default agent limit from 6 to 50. We changed
  batch claims to fill available slots with multiple tasks per agent and notify
  the runtime when an admin raises an agent's limit.

See the [hosted plugin guide](docs/engineering/hosted-workflow-plugins.md) for
configuration and API details. In [plugins/](plugins/README.md), we maintain
packages for parent wakeups, in-progress continuation and issue-comment cleanup.

## Frontend

- **Issue tree.** You can inspect parent/child issues, expand collapsed card
  stacks, and preview collaborators' messages and runs. We added context-usage
  rings and compression counts, plus saved pan and zoom positions.
- **Settings.** You can manage hosted plugin execution credentials and personal
  sub-issue following from workspace settings.
- **Onboarding.** We removed the acquisition-source survey and its follow-up popup.

## CLI and daemon

- **Plugin commands.** You can inspect execution credentials, enable or disable
  hosted execution, and configure installed plugins:

  ```sh
  multica workspace plugin-execution status WORKSPACE
  multica workspace plugin-execution enable WORKSPACE --token-stdin < /path/to/pat
  multica workspace plugin-execution disable WORKSPACE
  multica workspace plugin list WORKSPACE
  multica workspace plugin configure INSTALLATION_ID WORKSPACE --config-file prompts.json
  ```

- **Startup prompts.** We render the workspace template into each provider's
  context file and expose the issue ID through `MULTICA_ISSUE_ID`.
- **Context measurements.** We collect native context-token measurements in the
  provider adapters and write them to issue metadata for the tree indicators.
- **Execution slots.** We raised the daemon default from 20 to 50 and made it
  continue claiming work after a partial batch. New agents created through the
  CLI default to 50 concurrent tasks; use `--max-concurrent-tasks` to set a limit.

## Development

Build this checkout using the [contributing guide](CONTRIBUTING.md).
See [self-hosting](SELF_HOSTING.md) for deployment and
[CLI and daemon setup](CLI_AND_DAEMON.md) for runtime configuration.

## License and attribution

See the [Multica License](LICENSE) and [NOTICE](NOTICE) for terms and upstream
attribution. Index Labs develops the original
[Multica repository](https://github.com/multica-ai/multica).
