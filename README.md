# Automatic sub-issue following: UI snapshots

These Chrome screenshots show the actual `WorkspaceTab` React component rendered with the repository's test fixtures and shared UI styles. They are component previews, not captures of a running backend deployment.

- **Before:** upstream `main` at `d908e4882d9b10e1477b0eccd47a94d63a0e777c`.
- **After:** branch `feat/configure-subissue-following`, cherry-picking `c2d1dad5a9c18cf94c03a55eb802f122df3bc971` and `d6528cef386ac73f25694700adb4562a1e17da1e` from this fork.
- **On:** empty workspace settings; automatic following defaults to enabled.
- **Off:** `prevent_inherited_child_subscriptions: true`.

Fixture workspace: Test Workspace. No production account or issue data is included. Screenshot assets are kept on this separate branch so the implementation PR contains only the two feature commits.
