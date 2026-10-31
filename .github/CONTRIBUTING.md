# Contributing

Keep changes focused and do not commit
credentials, generated binaries, private services or build output. Do not rewrite published history.

Build instructions are in the root README. Release and signing details are in
[RELEASING.md](../scripts/RELEASING.md). Use `npm ci` for the frontend and Go versions
declared by the modules. Run the affected tests before submitting a pull request.

Describe the user-visible change and validation. Changes to `.github`, `.signpath`,
`scripts`, `installer` or update/bootstrap code need explicit maintainer review.
Describe meaningful changes in the PR using a short conventional change summary.
Maintainers record changes in their PENDING_CHANGES.md build journal and Pack Manager
handles commits after successful builds; external contributors need not create that private journal.

Contributions to the project's own code are under MIT. Third-party assets retain their
own licenses and must include verifiable origin and redistribution notices.
