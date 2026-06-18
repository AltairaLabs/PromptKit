package main

import (
	"fmt"
	"strings"

	"github.com/manifoldco/promptui"

	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

func collectInteractiveVariables(
	config *templates.TemplateConfig, tmpl *templates.Template,
) (*templates.TemplateConfig, error) {
	fmt.Println("🏟️  Welcome to PromptArena!")
	fmt.Println()
	fmt.Println("Let's set up your testing project.")
	fmt.Println()

	for i := range tmpl.Spec.Variables {
		v := &tmpl.Spec.Variables[i]
		if v.Name == projectNameVar {
			continue
		}

		value, err := promptForVariable(v)
		if err != nil {
			return nil, err
		}
		config.Variables[v.Name] = value
	}

	return config, nil
}

func promptForVariable(v *templates.Variable) (interface{}, error) {
	promptText := buildPromptText(v)

	switch v.Type {
	case "boolean":
		return promptForBoolean(promptText)
	case "select":
		return promptForSelect(promptText, v)
	case "array":
		return promptForArray(promptText, v)
	default:
		return promptForString(promptText, v)
	}
}

func buildPromptText(v *templates.Variable) string {
	promptText := v.Prompt
	if promptText == "" {
		promptText = v.Name
	}
	if v.Description != "" {
		promptText = fmt.Sprintf("%s (%s)", promptText, v.Description)
	}
	return promptText
}

func promptForBoolean(promptText string) (interface{}, error) {
	prompt := promptui.Prompt{
		Label:     promptText,
		IsConfirm: true,
	}
	_, err := prompt.Run()
	if err != nil && err != promptui.ErrAbort {
		return nil, err
	}
	return err != promptui.ErrAbort, nil
}

func promptForSelect(promptText string, v *templates.Variable) (interface{}, error) {
	prompt := promptui.Select{
		Label: promptText,
		Items: v.Options,
	}
	if v.Default != nil {
		defaultStr := fmt.Sprintf("%v", v.Default)
		for i, opt := range v.Options {
			if opt == defaultStr {
				prompt.CursorPos = i
				break
			}
		}
	}
	_, result, err := prompt.Run()
	if err != nil {
		return nil, err
	}
	return result, nil
}

func promptForArray(promptText string, v *templates.Variable) (interface{}, error) {
	defaultStr := getArrayDefaultString(v)
	prompt := promptui.Prompt{
		Label:   promptText + " (comma-separated)",
		Default: defaultStr,
	}
	result, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	items := strings.Split(result, ",")
	for i := range items {
		items[i] = strings.TrimSpace(items[i])
	}
	return items, nil
}

func getArrayDefaultString(v *templates.Variable) string {
	if v.Default == nil {
		return ""
	}
	if arr, ok := v.Default.([]interface{}); ok {
		strs := make([]string, len(arr))
		for i, item := range arr {
			strs[i] = fmt.Sprintf("%v", item)
		}
		return strings.Join(strs, ",")
	}
	return ""
}

func promptForString(promptText string, v *templates.Variable) (interface{}, error) {
	defaultStr := ""
	if v.Default != nil {
		defaultStr = fmt.Sprintf("%v", v.Default)
	}

	prompt := promptui.Prompt{
		Label:   promptText,
		Default: defaultStr,
	}
	result, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	if v.Type == "number" {
		return parseNumber(result)
	}

	return result, nil
}

func parseNumber(result string) (interface{}, error) {
	var num float64
	if n, _ := fmt.Sscanf(result, "%f", &num); n == 1 {
		return num, nil
	}
	return result, nil
}
