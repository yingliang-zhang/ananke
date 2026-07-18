# Contributing to Ananke

Ananke is a personal project, but contributions are welcome.

## Development Principles

1. **Machine-verifiable contracts.** Every invariant must have a test that
   can detect its violation — including controlled mutation gates.
2. **Architecture over tuning.** When a defect is found, fix the design, not
   the parameter.
3. **Evidence-backed claims.** No performance or correctness claim without
   a recorded command output.
4. **TDD.** RED before GREEN, always.

## Getting Started

```sh
git clone https://github.com/yingliang-zhang/ananke.git
cd ananke
```

Go 1.26+ is required for the core. Node 22+ is required for the desktop shell.

## License

By contributing, you agree that your contributions are licensed under the
Apache License 2.0.
