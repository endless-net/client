# Contract CI repetition policy

The `Test` workflow runs the complete contract scenario inventory once on each
of its eight OS/architecture runners for every main push and pull request.
This produces eight heavy contract jobs, not twenty-four. Verify, installation,
control-plane and container jobs keep their existing coverage.

For pre-release verification or flake diagnosis, manually run `Test` on `main`
with `contract_repetitions=3`. This runs three isolated passes per platform.
The default manual value is `1`. All selected reports must match the source SHA
and compiled scenario inventory, and every scenario must pass without skips.

Publication still requires three successful repetitions per platform from the
latest manual `Test` run on the exact source SHA. A routine one-pass push is not
release evidence. Run the three-pass check before requesting publication; a
subsequent source change requires a new check for that SHA. No release is
published merely by selecting three repetitions.

New runs continue to cancel older runs of this workflow on the same ref.
