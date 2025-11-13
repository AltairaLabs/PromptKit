package junit

import (
	"encoding/xml"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// JUnit XML structures following the standard schema
// Reference: https://llg.cubic.org/docs/junit/

// JUnitTestSuites represents the root element of JUnit XML
type JUnitTestSuites struct {
	XMLName    xml.Name          `xml:"testsuites"`
	Name       string            `xml:"name,attr"`
	Tests      int               `xml:"tests,attr"`
	Failures   int               `xml:"failures,attr"`
	Errors     int               `xml:"errors,attr"`
	Time       float64           `xml:"time,attr"`
	TestSuites []*JUnitTestSuite `xml:"testsuite"`
}

// JUnitTestSuite represents a group of related tests (typically by scenario)
type JUnitTestSuite struct {
	XMLName    xml.Name        `xml:"testsuite"`
	Name       string          `xml:"name,attr"`
	Tests      int             `xml:"tests,attr"`
	Failures   int             `xml:"failures,attr"`
	Errors     int             `xml:"errors,attr"`
	Time       float64         `xml:"time,attr"`
	Timestamp  string          `xml:"timestamp,attr"`
	TestCases  []JUnitTestCase `xml:"testcase"`
	Properties []JUnitProperty `xml:"properties>property,omitempty"`
}

// JUnitProperty represents a key-value property in test suite or test case
type JUnitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// JUnitTestCase represents a single test execution
type JUnitTestCase struct {
	XMLName    xml.Name        `xml:"testcase"`
	Name       string          `xml:"name,attr"`
	Classname  string          `xml:"classname,attr"`
	Time       float64         `xml:"time,attr"`
	Failure    *JUnitFailure   `xml:"failure,omitempty"`
	Error      *JUnitError     `xml:"error,omitempty"`
	Skipped    *JUnitSkipped   `xml:"skipped,omitempty"`
	SystemOut  *JUnitOutput    `xml:"system-out,omitempty"`
	SystemErr  *JUnitOutput    `xml:"system-err,omitempty"`
	Properties []JUnitProperty `xml:"properties>property,omitempty"`
}

// JUnitFailure represents a test failure (assertion failed, validation error, etc.)
type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// JUnitError represents a test error (exception, crash, etc.)
type JUnitError struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

// JUnitSkipped represents a skipped test
type JUnitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// JUnitOutput represents system-out or system-err content
type JUnitOutput struct {
	Content string `xml:",chardata"`
}

// ValidationError type alias for Arena validation errors
// This allows the junit package to work with Arena's validation errors
// without importing the runtime/types package directly in all functions
type ValidationError = types.ValidationError
