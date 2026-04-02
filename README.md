# gRPC Bazel Compatibility Matrix Tool

This project provides a tool to test the compatibility of various versions of Bazel, gRPC, Protobuf, `rules_go`, and `rules_cc`.

It automates the process of creating a test workspace, configuring dependencies via Bzlmod (`MODULE.bazel`), and running a Bazel build to verify that the specified versions work together.

## How it Works

1. **Version Discovery**: The tool can automatically discover the latest versions of gRPC, Protobuf, `rules_go`, and `rules_cc` from the Bazel Central Registry (BCR) or GitHub, or you can specify specific versions via command-line flags.
2. **Template Rendering**: It uses a template file (`templates/MODULE.bazel.tmpl`) to generate a `MODULE.bazel` file with the selected dependency versions.
3. **Workspace Setup**: It copies the sample protobuf project from the `proto/` directory into a temporary workspace.
4. **Execution**: It runs `bazel build //...` using the specified Bazel version (respecting `USE_BAZEL_VERSION` for Bazelisk).
5. **Logging**: Build logs for each combination are saved in the `logs/` directory.
6. **Reporting**: Results are aggregated and saved to `results.json` and rendered as a table in `results.md`.

## Usage

To run the tool with default settings:

```bash
./bazel-grpc
```

### Command Line Flags

You can customize the test matrix using the following flags:

| Flag | Description | Default |
| --- | --- | --- |
| `-bazel` | Comma-separated list of major Bazel versions to test | `7,8,9` |
| `-grpc` | Specific gRPC version to test | Latest from BCR |
| `-proto` | Specific Protobuf version to test | Latest from BCR |
| `-rules_go` | Specific `rules_go` version to test | Latest from BCR |
| `-rules_cc` | Specific `rules_cc` version to test | Latest from BCR |

Example specifying specific versions:

```bash
./bazel-grpc -grpc 1.76.0.bcr.1 -proto 33.1 -rules_go 0.59.0
```

## Results

The latest test results are recorded in [results.md](results.md).
