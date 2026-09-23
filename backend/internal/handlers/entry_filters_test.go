package handlers

import (
	"net/url"
	"reflect"
	"strconv"
	"testing"
)

func TestMinistryDecisionFiltersAreParameterized(t *testing.T) {
	query := url.Values{
		"decision_number":       {"МЦ-%_402"},
		"instruction_authority": {"security_council"},
	}
	conditions := []string{"1=1"}
	arguments := []interface{}{}
	argument := func(value interface{}) string {
		arguments = append(arguments, value)
		return "$" + strconv.Itoa(len(arguments))
	}

	appendEntryDetailFilters(query, &conditions, argument)

	wantConditions := []string{
		"1=1",
		"payload->>'decision_number' ILIKE '%'||$1||'%'",
		"payload->>'instruction_authority'=$2",
	}
	if !reflect.DeepEqual(conditions, wantConditions) {
		t.Fatalf("conditions = %#v, want %#v", conditions, wantConditions)
	}
	if !reflect.DeepEqual(arguments, []interface{}{"МЦ-%_402", "security_council"}) {
		t.Fatalf("arguments = %#v", arguments)
	}
}

func TestTeachingFiltersIncludeSemesterAndEducationLevel(t *testing.T) {
	query := url.Values{
		"education_level": {"master"},
		"semester":        {"10"},
	}
	conditions := []string{"1=1"}
	arguments := []interface{}{}
	argument := func(value interface{}) string {
		arguments = append(arguments, value)
		return "$" + strconv.Itoa(len(arguments))
	}

	appendEntryDetailFilters(query, &conditions, argument)

	wantConditions := []string{
		"1=1",
		"payload->>'education_level'=$1",
		"payload->>'semester'=$2",
	}
	if !reflect.DeepEqual(conditions, wantConditions) {
		t.Fatalf("conditions = %#v, want %#v", conditions, wantConditions)
	}
	if !reflect.DeepEqual(arguments, []interface{}{"master", "10"}) {
		t.Fatalf("arguments = %#v", arguments)
	}
}
