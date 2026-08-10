# Behavioral tests

Every directory containing `test.json` is a behavioral compiler fixture. The
suite is a normal Go package, so all fixtures run as part of:

```sh
go test ./...
```

The package builds `qkc` once in `TestMain`, copies every fixture to a temporary
directory, and gives it a private `XDG_CACHE_HOME`. Non-cache fixtures run in
parallel. Cache sequences and fixtures with `"serial": true` run serially.

`test.json` requires one of these modes: `run-pass`, `compile-pass`,
`compile-fail`, `run-fail`, or `cache-sequence`. It can also set
`compilerArgs`, `args`, `env`, `goos`, `goarch`, `timeout`, `stdoutRegex`, and
`stderrRegex`. A fixture can provide `stdin.txt`, `args.txt`, `stdout.txt`,
`stderr.txt`, and `exit-code.txt`. Each line of `args.txt` is one argument, so
spaces are preserved.

Output is exact by default. Temporary fixture paths are replaced with
`<CASE>`. Set a regex flag only for unavoidable platform-specific output. To
accept an intentional output change, run:

```sh
UPDATE_BEHAVIORAL=1 go test ./tests/behavioral -run TestBehavioral
```

Cache sequences retain the working tree and cache between ordered steps. A
step can replace files with `files`, choose a pass/fail `mode`, and assert exact
`stdout`, exact `stderr`, `exitCode`, `stdoutContains`, or
`stdoutNotContains`.

`features.json` is checked independently. Every listed feature must reference
an existing positive fixture and either an existing negative fixture or an
explicit `negativeNotApplicable` marker. Positive and negative references are
also checked against their fixture modes.
