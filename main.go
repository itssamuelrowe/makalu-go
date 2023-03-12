package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"

	colorjson "github.com/TylerBrock/colorjson"
	"github.com/yalp/jsonpath"
	"gopkg.in/yaml.v3"
)

type TestCase struct {
	Target string                 `yaml:"target"`
	In     map[string]interface{} `yaml:"in"`
	Out    map[string]interface{} `yaml:"out"`
}

type Error struct {
	message     string
	actualKey   string
	expectedKey string
	category    string
	entry       Entry
}

var errors []Error

type Context struct {
	variables map[string]interface{}
	steps     map[string]map[string]interface{}
}

func readVars(varsPath string) (map[string]interface{}, error) {
	buffer, err := ioutil.ReadFile(varsPath)
	if err != nil {
		return nil, err
	}
	variables := map[string]interface{}{}
	err = yaml.Unmarshal(buffer, variables)

	if err != nil {
		return nil, fmt.Errorf("in file %q: %w", varsPath, err)
	}

	return variables, nil
}

func readConf(path string) (*TestCase, error) {
	buffer, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	renderedTemplate := string(buffer) // mustache.Render(string(buffer), variables)

	testCase := &TestCase{}
	err = yaml.Unmarshal([]byte(renderedTemplate), testCase)
	if err != nil {
		return nil, fmt.Errorf("in file %q: %w", path, err)
	}

	return testCase, err
}

type EqualityOperatorCompartor func(
	actualValue interface{},
	expectedValue interface{},
	actualKey string,
	expectedKey string,
	inverse bool,
) bool

func checkType(value interface{}, expected string) {
	if reflect.TypeOf(value).String() != expected {
		fmt.Printf("Expected type %s, but got type %t!\n", expected, value)
	}
}

var equalityOperators map[string]map[string]EqualityOperatorCompartor

func init() {
	equalityOperators = map[string]map[string]EqualityOperatorCompartor{
		"string": {
			"string": func(
				actual0 interface{},
				expected0 interface{},
				actualKey string,
				expectedKey string,
				inverse bool,
			) bool {
				actual := actual0.(string)
				expected := expected0.(string)

				if strings.HasPrefix(expected, "$") {
					if inverse && expected == "$string" {
						errors = append(errors, Error{
							message:     "Unexpected value type",
							actualKey:   actualKey,
							expectedKey: expectedKey + ":" + expected,
							category:    "response_error",
						})
						return false
					} else if !inverse && expected != "$string" {

						errors = append(errors, Error{
							message:     "Unexpected value type",
							actualKey:   actualKey,
							expectedKey: expectedKey + ":" + expected,
							category:    "response_error",
						})
						return false

					}
					return true
				}

				if inverse && actual == expected {
					errors = append(errors, Error{
						message:     "Values are equal",
						actualKey:   actualKey,
						expectedKey: expectedKey,
						category:    "response_error",
					})
					return false
				} else if !inverse && actual != expected {
					errors = append(errors, Error{
						message:     "Values are not equal",
						actualKey:   actualKey,
						expectedKey: expectedKey,
						category:    "response_error",
					})
					return false
				}

				return true
			},
			"map[string]interface {}": func(
				actual0 interface{},
				expected0 interface{},
				parentActualKey string,
				parentExpectedKey string,
				inverse bool,
			) bool {
				actualValue := actual0.(string)
				expectedValue := expected0.(map[string]interface{})
				result := true

				for expectedKey := range expectedValue {
					operand2 := expectedValue[expectedKey]

					if strings.HasPrefix(expectedKey, "$") {
						result = result && operate(actualValue, expectedKey, operand2, parentActualKey, parentExpectedKey+"."+expectedKey)
					} else {
						errors = append(errors, Error{
							message:     "Cannot mix operators and fields",
							actualKey:   parentActualKey,
							expectedKey: parentExpectedKey,
							category:    "spec_error",
						})
						result = false
					}
				}

				return result
			},
		},
		"json.Number": {
			"string": func(
				actual0 interface{},
				expected0 interface{},
				actualKey string,
				expectedKey string,
				inverse bool,
			) bool {
				expected := expected0.(string)

				if strings.HasPrefix(expected, "$") {
					if expected != "$number" {
						errors = append(errors, Error{
							message:     "Unexpected value type",
							actualKey:   actualKey,
							expectedKey: expectedKey + ":" + expected,
							category:    "response_error",
						})
						return false
					}
					return true
				}

				errors = append(errors, Error{
					message:     "Values are not equal",
					actualKey:   actualKey,
					expectedKey: expectedKey,
					category:    "response_error",
				})
				return false
			},
			"int": func(
				actual0 interface{},
				expected0 interface{},
				actualKey string,
				expectedKey string,
				inverse bool,
			) bool {
				if actual, err := actual0.(json.Number).Int64(); err == nil {
					if int(actual) == expected0.(int) {
						return true
					}
				}

				errors = append(errors, Error{
					message:     "Values are not equal",
					actualKey:   actualKey,
					expectedKey: expectedKey,
					category:    "response_error",
				})
				return false
			},
			"map[string]interface {}": func(
				actual0 interface{},
				expected0 interface{},
				actualKey string,
				expectedKey string,
				inverse bool,
			) bool {
				actualValue := actual0.(json.Number)
				expectedValue := expected0.(map[string]interface{})
				result := true

				for expectedKey := range expectedValue {
					operand2 := expectedValue[expectedKey]

					if strings.HasPrefix(expectedKey, "$") {
						result = result && operate(actualValue, expectedKey, operand2, "", "")
					} else {
						errors = append(errors, Error{
							message:     "Cannot mix operators and fields",
							actualKey:   actualKey,
							expectedKey: expectedKey,
							category:    "spec_error",
						})
						result = false
					}
				}
				return result
			},
		},
	}
}

func executeIsOperator(
	operand1 interface{},
	operand2 string,
	actualKey string,
	expectedKey string,
	inverse bool,
) bool {
	expectedTypeName := strings.TrimPrefix(operand2, "$")
	actualTypeName := reflect.TypeOf(operand1).String()
	if inverse {
		if actualTypeName != expectedTypeName {
			return true
		} else {
			errors = append(errors, Error{
				message:     "Value type matched",
				actualKey:   actualKey,
				expectedKey: expectedKey,
				category:    "response_error",
			})
			return false
		}
	} else if !inverse {
		if actualTypeName == expectedTypeName {
			return true
		} else {
			errors = append(errors, Error{
				message:     "Value type mismatched",
				actualKey:   actualKey,
				expectedKey: expectedKey,
				category:    "response_error",
			})
			return false
		}
	}
	/* The control should never reach here. */
	return false
}

func executeNeOperator(
	actualValue interface{},
	expectedValue interface{},
	actualKey string,
	expectedKey string,
) bool {
	actualValueType := reflect.TypeOf(actualValue).String()
	expectedValueType := reflect.TypeOf(expectedValue).String()

	if comparator, okay := equalityOperators[actualValueType][expectedValueType]; okay {
		return comparator(actualValue, expectedValue, actualKey, expectedKey, true)
	}

	fmt.Printf("Makalu does not currently support %s vs %s comparisons!\n", actualValueType, expectedValueType)
	return false
}

func executeRegexOperator(
	actualValue0 interface{},
	expectedValue string,
	actualKey string,
	expectedKey string,
) bool {
	actualValueType := reflect.TypeOf(actualValue0).String()

	if actualValueType != "string" {
		errors = append(errors, Error{
			message:     "Unexpected value type",
			actualKey:   actualKey,
			expectedKey: expectedKey,
			category:    "response_error",
		})
		return false
	}

	actualValue := actualValue0.(string)
	matched, err := regexp.MatchString(expectedValue, actualValue)

	if err != nil {
		errors = append(errors, Error{
			message:     "Invalid regex pattern",
			actualKey:   actualKey,
			expectedKey: expectedKey,
			category:    "spec_error",
		})
		return false
	}

	if !matched {
		errors = append(errors, Error{
			message:     "Regex mismatch",
			actualKey:   actualKey,
			expectedKey: expectedKey,
			category:    "response_error",
		})
	}

	return matched
}

func operate(
	operand1 interface{},
	operator string,
	operand2 interface{},
	parentActualKey string,
	parentExpectedKey string,
) bool {
	switch operator {
	case "$is":
	case "$is_not":
		{
			typeName := reflect.TypeOf(operand2).String()
			if typeName != "string" || !strings.HasPrefix(operand2.(string), "$") {
				errors = append(errors, Error{
					message:     operator + " operator expects type name",
					actualKey:   parentActualKey,
					expectedKey: parentExpectedKey,
					category:    "spec_error",
				})
				return false
			}
			return executeIsOperator(
				operand1,
				operand2.(string),
				parentActualKey,
				parentActualKey+"."+operator,
				operator == "$is_not",
			)
		}
	case "$ne":
		{
			return executeNeOperator(operand1, operand2, parentActualKey, parentExpectedKey)
		}
	case "$regex":
		{
			if reflect.TypeOf(operand2).String() != "string" {
				errors = append(errors, Error{
					message:     "$regex operator expects regex pattern",
					actualKey:   parentActualKey,
					expectedKey: parentExpectedKey,
					category:    "spec_error",
				})
				return false
			}
			return executeRegexOperator(operand1, operand2.(string), parentActualKey, parentExpectedKey)
		}
	}

	return false
}

func compareObjects(
	actual map[string]interface{},
	expected map[string]interface{},
	parentActualKey string,
	parentExpectedKey string,
) {
	for key := range actual {
		optionalKey := key + "?"
		_, keyExists := expected[key]
		_, optionalKeyExists := expected[optionalKey]

		if !keyExists && !optionalKeyExists {
			errors = append(errors, Error{
				message:     "Unknown key " + key,
				actualKey:   parentActualKey + "." + key,
				expectedKey: parentExpectedKey + ".$unknown",
				category:    "match_error",
			})
		}
	}

	for expectedKey := range expected {
		optional := false
		actualKey := expectedKey

		/* Determine whether this key is optional. */
		if strings.HasSuffix(expectedKey, "?") {
			actualKey = strings.TrimSuffix(expectedKey, "?")
			optional = true
		}

		actualValue, actualValueExists := actual[actualKey]
		if !actualValueExists {
			if !optional {
				errors = append(errors, Error{
					message:     "Cannot find required key '" + actualKey + "'",
					actualKey:   parentActualKey + ".<" + actualKey + ">",
					expectedKey: parentExpectedKey + "." + expectedKey,
					category:    "match_error",
				})
			}
			continue
		}

		expectedValue := expected[expectedKey]

		if strings.HasPrefix(expectedKey, "$") {
			operate(actualValue, expectedKey, expectedValue, parentActualKey+"."+actualKey, parentExpectedKey+"."+expectedKey)
		} else {
			actualValueType := reflect.TypeOf(actualValue).String()
			expectedValueType := reflect.TypeOf(expectedValue).String()

			if comparator, okay := equalityOperators[actualValueType][expectedValueType]; okay {
				comparator(
					actualValue,
					expectedValue,
					parentActualKey+"."+actualKey,
					parentExpectedKey+"."+expectedKey,
					false,
				)
			} else {
				fmt.Printf(
					"Makalu does not currently support %s vs %s comparisons!\n",
					actualValueType,
					expectedValueType,
				)
			}
		}
	}
}

func printResponse(object map[string]interface{}) {
	formatter := colorjson.NewFormatter()
	formatter.Indent = 4

	prettyJsonAsString, _ := formatter.Marshal(object)
	fmt.Println(string(prettyJsonAsString))
}

type Entry struct {
	longName  string
	shortName string
}

func listFiles(longRoot string, shortRoot string, list *[]Entry) {
	entries, err := os.ReadDir(longRoot)
	if err != nil {
		log.Fatal(err)
	}
	for _, entry := range entries {
		longName := longRoot + string(os.PathSeparator) + entry.Name()
		shortName := shortRoot + string(os.PathSeparator) + entry.Name()
		if entry.IsDir() {
			listFiles(longName, shortName, list)
		} else {
			if shortName != "."+string(os.PathSeparator)+"vars.yaml" {
				*list = append(*list, Entry{
					longName:  longName,
					shortName: shortName,
				})
			}
		}
	}
}

func refer(value string, context map[string]interface{}) interface{} {
	var buffer bytes.Buffer

	for index := 0; index < len(value); {
		startIndex := strings.Index(value, "{{")
		if startIndex >= index {
			buffer.WriteString(value[index:startIndex])
			stopIndex := strings.Index(value, "}}")
			if stopIndex >= startIndex+2 {
				snippet := strings.TrimSpace(value[startIndex+2 : stopIndex])

				fmt.Printf("%v %v", snippet, context)

				if snippet == "" {
					// error
				}

				value, err := jsonpath.Read(context, "$."+snippet)
				if err != nil {
					log.Fatal(err)
				}
				buffer.WriteString(value.(string))
				index = stopIndex + 2
			}
		} else {
			buffer.WriteString(value[index:])
			index = len(value)
		}
	}
	return buffer.String()
}

func processEntry(entry Entry, context map[string]interface{}) {
	testCase, err := readConf(entry.longName)
	if err != nil {
		log.Fatal(err)
	}

	cleanTarget := strings.TrimSpace(testCase.Target)
	if cleanTarget == "" {
		errors = append(errors, Error{
			message:     "Target expected",
			actualKey:   "",
			expectedKey: "$root.target",
			category:    "spec_error",
			entry:       entry,
		})
		return
	}

	target := refer(testCase.Target, context).(string)
	fmt.Printf("[*] Executing '%s'\n", target)
	parts := strings.Split(target, " ")
	method := parts[0]
	url := parts[1]

	if method == "GET" {
		response, err := http.Get(url)

		if err != nil {
			log.Fatalf("An error occured:\n%v", err)
		}
		defer response.Body.Close()

		responseBody, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Fatalln(err)
		}

		var responseObject map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(responseBody)))
		decoder.UseNumber()
		decoder.Decode(&responseObject)

		printResponse(responseObject)

		compareObjects(responseObject, testCase.Out, "$root", "$root.out")
	}

	if method == "POST" {
		body, _ := json.Marshal(testCase.In)
		buffer := bytes.NewBuffer(body)
		response, err := http.Post(url, "application/json", buffer)

		if err != nil {
			log.Fatalf("An error occured:\n%v", err)
		}
		defer response.Body.Close()

		responseBody, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Fatalln(err)
		}

		responseAsString := string(responseBody)

		var responseObject map[string]any
		decoder := json.NewDecoder(strings.NewReader(responseAsString))
		decoder.UseNumber()
		decoder.Decode(&responseObject)

		printResponse(responseObject)

		compareObjects(responseObject, testCase.Out, "$root", "$root.out")
	}
}

func isPathValid(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	var entries []Entry
	listFiles(cwd, ".", &entries)

	varsPath := cwd + string(os.PathSeparator) + "vars.yaml"
	valid, err := isPathValid(varsPath)
	if err != nil {
		log.Fatal(err)
	}

	var context = map[string]interface{}{}

	if valid {
		variables, err := readVars(varsPath)
		if err != nil {
			log.Fatal(err)
		}

		context["vars"] = variables
	}

	for _, entry := range entries {
		processEntry(entry, context)
	}

	if len(errors) > 0 {
		fmt.Println()
	}

	for index, item := range errors {
		fmt.Printf("%s\n[%s] %s\n    actual path   -- %s\n    expected path -- %s\n",
			item.entry.shortName,
			item.category,
			item.message,
			item.actualKey,
			item.expectedKey,
		)
		if index+1 < len(errors) {
			fmt.Println()
		}
	}

}
