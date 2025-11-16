# Tools

This directory contains tool definitions for function calling in PromptKit Arena.

## K8s-Style Tool Configuration

Tools are defined using Kubernetes-style manifests with the following structure:

```yaml
apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: tool-name  # Used as the tool identifier
spec:
  description: Human-readable description of what the tool does
  input_schema:
    # JSON Schema Draft-07 for validating input arguments
    type: object
    properties:
      param_name:
        type: string
        description: Parameter description
    required:
      - param_name
  output_schema:
    # JSON Schema Draft-07 for validating output
    type: object
    properties:
      result_field:
        type: string
  mode: mock  # "mock" or "live"
  timeout_ms: 2000
  mock_result:  # Static mock data (for mode: mock)
    result_field: "example value"
```

## Fields

- **metadata.name**: The tool identifier used in scenarios and by LLMs
- **spec.description**: Clear description of the tool's purpose and behavior
- **spec.input_schema**: JSON Schema defining valid input arguments
- **spec.output_schema**: JSON Schema defining expected output format
- **spec.mode**: Execution mode - "mock" for testing or "live" for real API calls
- **spec.timeout_ms**: Maximum execution time in milliseconds
- **spec.mock_result**: Static mock response (when mode is "mock")
- **spec.mock_template**: Template for dynamic mocks (optional)
- **spec.http**: HTTP configuration for live API calls (when mode is "live")

## Example

See `example-tool.yaml` for a complete example of a customer support tool.

## Loading Tools

Tools are loaded through the arena configuration:

```yaml
spec:
  tools:
    - file: tools/my-tool.yaml
    - file: tools/another-tool.yaml
```

Tools are resolved relative to the arena configuration file's directory (or the `config_dir` setting).
