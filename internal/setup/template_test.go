package setup

import (
	"os"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestCallerTemplateWorkflowDispatchAndConcurrency(t *testing.T) {
	data, err := os.ReadFile("../../caller-template.yml")
	if err != nil {
		t.Fatal(err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	root := mappingValue(t, document.Content[0], "on")
	workflowDispatch := mappingValue(t, root, "workflow_dispatch")
	inputs := mappingValue(t, workflowDispatch, "inputs")
	for _, name := range []string{"from", "to"} {
		input := mappingValue(t, inputs, name)
		if scalarValue(t, input, "type") != "string" {
			t.Fatalf("workflow_dispatch input %q is not a string", name)
		}
		if scalarValue(t, input, "required") != "true" {
			t.Fatalf("workflow_dispatch input %q is not required", name)
		}
	}

	jobs := mappingValue(t, document.Content[0], "jobs")
	commitlint := mappingValue(t, jobs, "commitlint")
	steps := mappingValue(t, commitlint, "steps")
	if len(steps.Content) == 0 {
		t.Fatal("commitlint job has no steps")
	}
	with := mappingValue(t, steps.Content[0], "with")
	if scalarValue(t, with, "from") != "${{ inputs.from }}" || scalarValue(t, with, "to") != "${{ inputs.to }}" {
		t.Fatalf("action does not receive workflow dispatch inputs")
	}

	concurrency := mappingValue(t, document.Content[0], "concurrency")
	if scalarValue(t, concurrency, "group") != "commitlint-${{ github.workflow }}-${{ github.event.pull_request.number || github.run_id }}" {
		t.Fatal("concurrency group is not scoped to the workflow and PR with run ID fallback")
	}
	if scalarValue(t, concurrency, "cancel-in-progress") != "${{ github.event_name == 'pull_request' }}" {
		t.Fatal("concurrency cancellation is not limited to pull requests")
	}
}

func TestGeneratedWorkflowPreservesIndependentPushChecks(t *testing.T) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(workflow("v0.3.0", "commits")), &document); err != nil {
		t.Fatal(err)
	}
	jobs := mappingValue(t, document.Content[0], "jobs")
	job := mappingValue(t, jobs, "commit-guard")
	concurrency := mappingValue(t, job, "concurrency")
	if scalarValue(t, concurrency, "group") != "commit-guard-${{ github.workflow }}-${{ github.event.pull_request.number || github.run_id }}" || scalarValue(t, concurrency, "cancel-in-progress") != "${{ github.event_name == 'pull_request' }}" {
		t.Fatal("generated workflow can cancel unrelated checks")
	}
}

func mappingValue(t *testing.T, mapping *yaml.Node, key string) *yaml.Node {
	t.Helper()
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		t.Fatalf("expected mapping for %q", key)
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	t.Fatalf("missing mapping key %q", key)
	return nil
}

func scalarValue(t *testing.T, mapping *yaml.Node, key string) string {
	t.Helper()
	node := mappingValue(t, mapping, key)
	if node.Kind != yaml.ScalarNode {
		t.Fatalf("expected scalar value for %q", key)
	}
	return node.Value
}
