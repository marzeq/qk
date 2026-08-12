# Behavioral tests

Every directory containing `test.json` is a behavioral compiler fixture. The
suite is a normal Go package, so all fixtures run as part of:

```sh
go test ./...
```

The package builds `qkc` once in `TestMain` and copies every fixture to a
temporary directory. Fixtures run in parallel unless `"serial": true`.
Successful fixture results are cached independently. A fixture runs again when
its files change, or when the compiler, external libraries, test harness,
platform, or cache format changes. Set `QK_BEHAVIORAL_CACHE=off` to force every
selected fixture to run. Snapshot-update runs always bypass the result cache.

`test.json` requires one of these modes: `run-pass`, `compile-pass`,
`compile-fail`, or `run-fail`. It can also set
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

`features.json` is checked independently. Every listed feature must reference
an existing positive fixture and either an existing negative fixture or an
explicit `negativeNotApplicable` marker. Positive and negative references are
also checked against their fixture modes.
