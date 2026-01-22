package sandbox_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/haasonsaas/nexus/internal/tools/sandbox"
)

// Example_basicUsage demonstrates basic code execution.
func Example_basicUsage() {
	// Create executor
	executor, err := sandbox.NewExecutor()
	if err != nil {
		log.Fatal(err)
	}
	defer executor.Close()

	// Execute Python code
	params := sandbox.ExecuteParams{
		Language: "python",
		Code:     `print("Hello, World!")`,
		Timeout:  10,
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := executor.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Content)
}

// Example_withFiles demonstrates file mounting.
func Example_withFiles() {
	executor, err := sandbox.NewExecutor()
	if err != nil {
		log.Fatal(err)
	}
	defer executor.Close()

	params := sandbox.ExecuteParams{
		Language: "python",
		Code: `
with open('config.json', 'r') as f:
    import json
    config = json.load(f)
    print(f"App: {config['app']}, Version: {config['version']}")
`,
		Files: map[string]string{
			"config.json": `{"app": "nexus", "version": "1.0.0"}`,
		},
		Timeout: 10,
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := executor.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Content)
}

// Example_multipleLanguages demonstrates executing different languages.
func Example_multipleLanguages() {
	executor, err := sandbox.NewExecutor()
	if err != nil {
		log.Fatal(err)
	}
	defer executor.Close()

	languages := []struct {
		lang string
		code string
	}{
		{"python", `print("Hello from Python")`},
		{"nodejs", `console.log("Hello from Node.js")`},
		{"bash", `echo "Hello from Bash"`},
	}

	for _, test := range languages {
		params := sandbox.ExecuteParams{
			Language: test.lang,
			Code:     test.code,
			Timeout:  10,
		}

		paramsJSON, _ := json.Marshal(params)
		result, err := executor.Execute(context.Background(), paramsJSON)
		if err != nil {
			log.Printf("Error executing %s: %v", test.lang, err)
			continue
		}

		fmt.Printf("%s: %s\n", test.lang, result.Content)
	}
}

// Example_withResourceLimits demonstrates custom resource limits.
func Example_withResourceLimits() {
	executor, err := sandbox.NewExecutor()
	if err != nil {
		log.Fatal(err)
	}
	defer executor.Close()

	params := sandbox.ExecuteParams{
		Language: "python",
		Code: `
import time
for i in range(5):
    print(f"Iteration {i}")
    time.sleep(0.1)
`,
		Timeout:  5,   // 5 second timeout
		CPULimit: 500, // 0.5 cores
		MemLimit: 256, // 256 MB
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := executor.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Content)
}

// Example_poolManagement demonstrates pool operations.
func Example_poolManagement() {
	executor, err := sandbox.NewExecutor(
		sandbox.WithPoolSize(2),
		sandbox.WithMaxPoolSize(5),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer executor.Close()

	// Get pool statistics
	// Note: This accesses internal pool - for demonstration only
	// In production, you'd use metrics/monitoring

	// Execute some code to warm up the pool
	params := sandbox.ExecuteParams{
		Language: "python",
		Code:     `print("test")`,
		Timeout:  10,
	}

	paramsJSON, _ := json.Marshal(params)
	_, err = executor.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Pool is ready")
}

// Example_errorHandling demonstrates error handling.
func Example_errorHandling() {
	executor, err := sandbox.NewExecutor()
	if err != nil {
		log.Fatal(err)
	}
	defer executor.Close()

	// Code with syntax error
	params := sandbox.ExecuteParams{
		Language: "python",
		Code:     `print("unclosed string`,
		Timeout:  10,
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := executor.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	if result.IsError {
		fmt.Println("Execution failed (expected)")
		fmt.Println("Error output captured in result.Content")
	}
}

// Example_stdin demonstrates providing input to programs.
func Example_stdin() {
	executor, err := sandbox.NewExecutor()
	if err != nil {
		log.Fatal(err)
	}
	defer executor.Close()

	params := sandbox.ExecuteParams{
		Language: "python",
		Code: `
import sys
name = sys.stdin.read().strip()
print(f"Hello, {name}!")
`,
		Stdin:   "Nexus",
		Timeout: 10,
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := executor.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Content)
}

// Example_dataProcessing demonstrates a more complex data processing task.
func Example_dataProcessing() {
	executor, err := sandbox.NewExecutor()
	if err != nil {
		log.Fatal(err)
	}
	defer executor.Close()

	params := sandbox.ExecuteParams{
		Language: "python",
		Code: `
import json

# Read data file
with open('data.json', 'r') as f:
    data = json.load(f)

# Process data
total = sum(item['value'] for item in data['items'])
avg = total / len(data['items'])

# Output results
print(f"Total: {total}")
print(f"Average: {avg:.2f}")
print(f"Count: {len(data['items'])}")
`,
		Files: map[string]string{
			"data.json": `{
				"items": [
					{"name": "A", "value": 10},
					{"name": "B", "value": 20},
					{"name": "C", "value": 30}
				]
			}`,
		},
		Timeout: 10,
	}

	paramsJSON, _ := json.Marshal(params)
	result, err := executor.Execute(context.Background(), paramsJSON)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Content)
}
