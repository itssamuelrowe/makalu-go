package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

type TestCase struct {
	Target string                 `yaml:"target"`
	In     map[string]interface{} `yaml:"in"`
	Out    map[string]interface{} `yaml:"out"`
}

func readConf(path string) (*TestCase, error) {
	buffer, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	testCase := &TestCase{}
	err = yaml.Unmarshal(buffer, testCase)
	if err != nil {
		return nil, fmt.Errorf("in file %q: %w", path, err)
	}

	return testCase, err
}

type Comparator func(actual interface{}, expected interface{}) bool

func checkType(value interface{}, expected string) {
	if reflect.TypeOf(value).String() != expected {
		fmt.Printf("Expected type %s, but got type %t!\n", expected, value)
	}
}

var comparators map[string]map[string]Comparator

func init() {
	comparators = map[string]map[string]Comparator{
		"string": {
			"string": func(actual0 interface{}, expected0 interface{}) bool {
				actual := actual0.(string)
				expected := expected0.(string)

				if strings.HasPrefix(expected, "$") {
					if expected != "$string" {
						fmt.Printf("Expected: %s, but received %v", expected, actual0)
						return false
					}
					return true
				}

				if actual != expected {
					fmt.Printf("Expected: %s, but received %v", expected, actual0)
					return false
				}

				return true
			},
			"map[string]interface {}": func(actual0 interface{}, expected0 interface{}) bool {
				actualValue := actual0.(string)
				expectedValue := expected0.(map[string]interface{})

				for expectedKey := range expectedValue {
					operand2 := expectedValue[expectedKey]

					if strings.HasPrefix(expectedKey, "$") {
						return operate(actualValue, expectedKey, operand2)
					} else {
						fmt.Printf("[error] Cannot mix both operators and object fields.")
					}
				}
				return false
			},
		},
		"json.Number": {
			"string": func(actual0 interface{}, expected0 interface{}) bool {
				expected := expected0.(string)

				if strings.HasPrefix(expected, "$") {
					if expected != "$number" {
						fmt.Printf("Expected: %s, but received %v", expected, actual0)
						return false
					}
					return true
				}

				return false
			},
			"int": func(actual0 interface{}, expected0 interface{}) bool {
				if actual, err := actual0.(json.Number).Int64(); err == nil {
					if int(actual) == expected0.(int) {
						return true
					}
				}
				fmt.Printf("Expected: %v, but received %v", expected0, actual0)
				return false
			},
			"map[string]interface {}": func(actual0 interface{}, expected0 interface{}) bool {
				actualValue := actual0.(json.Number)
				expectedValue := expected0.(map[string]interface{})

				for expectedKey := range expectedValue {
					operand2 := expectedValue[expectedKey]

					if strings.HasPrefix(expectedKey, "$") {
						return operate(actualValue, expectedKey, operand2)
					} else {
						fmt.Printf("[error] Cannot mix both operators and object fields.")
					}
				}
				return false
			},
		},
	}
}

func executeIsOperator(operand1 interface{}, operand2 string) bool {
	typeName := strings.TrimPrefix(operand2, "$")
	if reflect.TypeOf(operand1).String() == typeName {
		return true
	}

	fmt.Printf("Expected %s, but received %v\n", operand2, operand1)
	return false
}

func executeNeOperator(actualValue interface{}, expectedValue interface{}) bool {
	actualValueType := reflect.TypeOf(actualValue).String()
	expectedValueType := reflect.TypeOf(expectedValue).String()

	if comparator, okay := comparators[actualValueType][expectedValueType]; okay {
		if comparator(actualValue, expectedValue) {
			fmt.Printf("Condition $ne failed! actual value %v == expected value %v", actualValue, expectedValue)
			return false
		}

		return true
	} else {
		fmt.Printf("Makalu does not currently support %s vs %s comparisons!\n", actualValueType, expectedValueType)
	}
	return false
}

func operate(operand1 interface{}, operator string, operand2 interface{}) bool {
	switch operator {
	case "$is":
		{
			if reflect.TypeOf(operand2).String() != "string" {
				fmt.Println("[error] $is operator expects type name")
				return false
			}
			return executeIsOperator(operand1, operand2.(string))
		}
	case "$ne":
		{
			return executeNeOperator(operand1, operand2)
		}
	}

	return false
}

func compareObjects(actual map[string]interface{}, expected map[string]interface{}, parent string) {
	for key := range actual {
		optionalKey := key + "?"
		_, keyExists := expected[key]
		_, optionalKeyExists := expected[optionalKey]

		if !keyExists && !optionalKeyExists {
			fmt.Printf("[error] Unknown key '%s'\n", key)
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
				fmt.Printf("[error] Cannot find required key '%s'\n", expectedKey)
			}
			continue
		}

		expectedValue := expected[expectedKey]

		if strings.HasPrefix(expectedKey, "$") {
			operate(actualValue, expectedKey, expectedValue)
		} else {
			actualValueType := reflect.TypeOf(actualValue).String()
			expectedValueType := reflect.TypeOf(expectedValue).String()

			if comparator, okay := comparators[actualValueType][expectedValueType]; okay {
				if comparator(actualValue, expectedValue) {
				}
			} else {
				fmt.Printf("Makalu does not currently support %s vs %s comparisons!\n", actualValueType, expectedValueType)
			}
		}
	}
}

func main() {
	testCase, err := readConf("test.yaml")
	if err != nil {
		log.Fatal(err)
	}

	parts := strings.Split(testCase.Target, " ")
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

		compareObjects(responseObject, testCase.Out, "$root")
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

		var responseObject map[string]any
		json.Unmarshal([]byte(string(responseBody)), &responseObject)

		// compareObjects(responseObject, testCase.Out)

		fmt.Println(reflect.TypeOf(testCase.Out))
	}

	// fmt.Printf("%#v", testCase)
}
