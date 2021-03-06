/*
Copyright 2019 Intel Corporation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rdt

import (
	"io/ioutil"
	stdlog "log"
	"os"
	"os/exec"
	"path/filepath"
	"sigs.k8s.io/yaml"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/marquiz/goresctrl/pkg/utils"
	testdata "github.com/marquiz/goresctrl/test/data"
)

const mockGroupPrefix string = "goresctrl."

type mockResctrlFs struct {
	t *testing.T

	origDir string
	baseDir string
}

func newMockResctrlFs(t *testing.T, name, mountOpts string) (*mockResctrlFs, error) {
	var err error
	m := &mockResctrlFs{t: t}

	m.origDir = testdata.Path(name)
	m.baseDir, err = ioutil.TempDir("", "goresctrl.test.")
	if err != nil {
		return nil, err
	}

	// Create resctrl filesystem mock
	m.copyFromOrig("", "")

	// Create mountinfo mock
	mountInfoPath = filepath.Join(m.baseDir, "mounts")
	resctrlPath := filepath.Join(m.baseDir, "resctrl")
	data := "resctrl " + resctrlPath + " resctrl " + mountOpts + " 0 0\n"
	if err := ioutil.WriteFile(mountInfoPath, []byte(data), 0644); err != nil {
		m.delete()
		return nil, err
	}
	return m, nil
}

func (m *mockResctrlFs) delete() {
	if err := os.RemoveAll(m.baseDir); err != nil {
		m.t.Fatalf("failed to delete mock resctrl fs: %v", err)
	}
}

func (m *mockResctrlFs) initMockMonGroup(class, name string) {
	m.copyFromOrig(filepath.Join("mon_groups", "example"), filepath.Join(mockGroupPrefix+class, "mon_groups", mockGroupPrefix+name))
}

func (m *mockResctrlFs) copyFromOrig(relSrc, relDst string) {
	absSrc := filepath.Join(m.origDir, relSrc)
	if s, err := os.Stat(absSrc); err != nil {
		m.t.Fatalf("%v", err)
	} else if s.IsDir() {
		absSrc = filepath.Join(absSrc, ".")
	}

	absDst := filepath.Join(m.baseDir, "resctrl", relDst)
	cmd := exec.Command("cp", "-r", absSrc, absDst)
	if err := cmd.Run(); err != nil {
		m.t.Fatalf("failed to copy mock data %q -> %q: %v", absSrc, absDst, err)
	}
}

func (m *mockResctrlFs) verifyTextFile(relPath, content string) {
	verifyTextFile(m.t, filepath.Join(m.baseDir, "resctrl", relPath), content)
}

func verifyTextFile(t *testing.T, path, content string) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		t.Errorf("failed to read %q: %v", path, err)
	}
	if string(data) != content {
		t.Errorf("unexpected content in %q\nexpected:\n  %q\nfound:\n  %q", path, content, data)
	}
}

func parseTestConfig(t *testing.T, data string) *Config {
	c := &Config{}
	if err := yaml.Unmarshal([]byte(data), c); err != nil {
		t.Fatalf("failed to parse rdt config: %v", err)
	}
	return c
}

const rdtTestConfig string = `
config:
  l3:
    optional: false
  mb:
    optional: false
partitions:
  priority:
    l3Allocation:
      all: 60%
    mbAllocation:
      all: [100%]
    classes:
      Guaranteed:
        l3schema:
          all: 100%
  default:
    l3Allocation:
      all: 40%
    mbAllocation:
      all: [100%]
    classes:
      Burstable:
        l3schema:
          all: 100%
        mbschema:
          all: [66%]
      BestEffort:
        l3schema:
          all: 66%
        mbschema:
          all: [33%]
`

// TestRdt tests the rdt public API, i.e. exported functionality of the package
func TestRdt(t *testing.T) {
	verifyGroupNames := func(a interface{}, b []string) {
		var names []string

		switch v := a.(type) {
		case []CtrlGroup:
			for _, g := range v {
				names = append(names, g.Name())
			}
		case []MonGroup:
			for _, g := range v {
				names = append(names, g.Name())
			}
		default:
			t.Errorf("Invalid type '%T' in verifyGroupNames()", a)
			return
		}
		if len(b) == 0 && len(names) == 0 {
			return
		}
		if !cmp.Equal(names, b) {
			t.Errorf("unexpected class/group names: expected %s got %s", b, names)
		}
	}

	// Set group remove function so that mock groups can be removed
	groupRemoveFunc = os.RemoveAll

	//
	// 1. test uninitialized interface
	//
	SetLogger(NewLoggerWrapper(stdlog.New(os.Stderr, "[ rdt-test-1 ] ", 0)))

	if err := SetConfig(&Config{}); err == nil {
		t.Errorf("setting config on uninitialized rdt succeeded unexpectedly")

	}
	if classes := GetClasses(); len(classes) != 0 {
		t.Errorf("uninitialized rdt contains classes %s", classes)
	}
	if _, ok := GetClass(""); ok {
		t.Errorf("expected to not get a class with empty name")
	}
	if MonSupported() {
		t.Errorf("unitialized rdt claims monitoring to be supported")
	}
	if features := GetMonFeatures(); len(features) != 0 {
		t.Errorf("uninitialized rdt returned monitoring features %s", features)
	}

	//
	// 2. Test setting up RDT with L3 L3_MON and MB support
	//
	mockFs, err := newMockResctrlFs(t, "resctrl.full", "")
	if err != nil {
		t.Fatalf("failed to set up mock resctrl fs: %v", err)
	}
	defer mockFs.delete()

	conf := parseTestConfig(t, rdtTestConfig)
	if err := Initialize(mockGroupPrefix); err != nil {
		t.Fatalf("rdt initialization failed: %v", err)
	}

	// Check that existing groups were read correctly on init
	classes := GetClasses()
	verifyGroupNames(classes, []string{"Guaranteed", "SYSTEM_DEFAULT", "Stale"})

	cls, _ := GetClass("SYSTEM_DEFAULT")
	verifyGroupNames(cls.GetMonGroups(), []string{})
	cls, _ = GetClass("Guaranteed")
	verifyGroupNames(cls.GetMonGroups(), []string{"predefined_group_live"})

	// Check that confiouration succeeds
	if err := SetConfig(conf); err != nil {
		t.Fatalf("rdt configuration failed: %v", err)
	}

	// Check that SetLogger() takes effect in the control interface, too
	SetLogger(NewLoggerWrapper(stdlog.New(os.Stderr, "[ rdt-test-2 ] ", 0)))
	if p := rdt.Logger.(*logger).Prefix(); p != "[ rdt-test-2 ] " {
		t.Errorf("unexpected logger prefix %q", p)
	}

	// Check that the path() and relPath() methods work correctly
	if p := rdt.classes["Guaranteed"].path("foo"); p != filepath.Join(mockFs.baseDir, "resctrl", "goresctrl.Guaranteed", "foo") {
		t.Errorf("path() returned wrong path %q", p)
	}
	if p := rdt.classes["Guaranteed"].relPath("foo"); p != filepath.Join("goresctrl.Guaranteed", "foo") {
		t.Errorf("relPath() returned wrong path %q", p)
	}

	// Verify that ctrl groups are correctly configured
	mockFs.verifyTextFile(rdt.classes["BestEffort"].relPath("schemata"),
		"L3:0=3f;1=3f;2=3f;3=3f\nMB:0=33;1=33;2=33;3=33\n")
	mockFs.verifyTextFile(rdt.classes["Burstable"].relPath("schemata"),
		"L3:0=ff;1=ff;2=ff;3=ff\nMB:0=66;1=66;2=66;3=66\n")
	mockFs.verifyTextFile(rdt.classes["Guaranteed"].relPath("schemata"),
		"L3:0=fff00;1=fff00;2=fff00;3=fff00\nMB:0=100;1=100;2=100;3=100\n")

	// Verify that existing goresctrl monitor groups were removed
	for _, cls := range []string{RootClassName, "Guaranteed"} {
		files, _ := ioutil.ReadDir(rdt.classes[cls].path("mon_groups"))
		for _, f := range files {
			if strings.HasPrefix(mockGroupPrefix, f.Name()) {
				t.Errorf("unexpected monitor group found %q", f.Name())
			}
		}
	}

	// Verify GetClasses
	classes = GetClasses()
	verifyGroupNames(classes, []string{"BestEffort", "Burstable", "Guaranteed", "SYSTEM_DEFAULT"})

	// Verify assigning pids to classes (ctrl groups)
	cls, _ = GetClass("Guaranteed")
	if n := cls.Name(); n != "Guaranteed" {
		t.Errorf("CtrlGroup.Name() returned %q, expected %q", n, "Guaranteed")
	}

	pids := []string{"10", "11", "12"}
	if err := cls.AddPids(pids...); err != nil {
		t.Errorf("AddPids() failed: %v", err)
	}
	if p, err := cls.GetPids(); err != nil {
		t.Errorf("GetPids() failed: %v", err)
	} else if !cmp.Equal(p, pids) {
		t.Errorf("GetPids() returned %s, expected %s", p, pids)
	}

	mockFs.verifyTextFile(rdt.classes["Guaranteed"].relPath("tasks"), "10\n11\n12\n")

	// Test creating monitoring groups
	cls, _ = GetClass("Guaranteed")
	mgName := "test_group"
	mgAnnotations := map[string]string{"a_key": "a_value"}
	mg, err := cls.CreateMonGroup(mgName, mgAnnotations)
	if err != nil {
		t.Errorf("creating mon group failed: %v", err)
	}
	if n := mg.Name(); n != mgName {
		t.Errorf("MonGroup.Name() returned %q, expected %q", n, mgName)
	}
	if a := mg.GetAnnotations(); !cmp.Equal(a, mgAnnotations) {
		t.Errorf("MonGroup.GetAnnotations() returned %s, expected %s", a, mgAnnotations)
	}
	if n := mg.Parent().Name(); n != "Guaranteed" {
		t.Errorf("MonGroup.Parent().Name() returned %q, expected %q", n, "Guaranteed")
	}

	if _, ok := cls.GetMonGroup("non-existing-group"); ok {
		t.Errorf("unexpected success when querying non-existing group")
	}
	if _, ok := cls.GetMonGroup(mgName); !ok {
		t.Errorf("unexpected error when querying mon group: %v", err)
	}

	verifyGroupNames(cls.GetMonGroups(), []string{"predefined_group_live", mgName})

	mgPath := rdt.classes["Guaranteed"].path("mon_groups", "goresctrl."+mgName)
	if _, err := os.Stat(mgPath); err != nil {
		t.Errorf("mon group directory not found: %v", err)
	}

	// Check that the monGroup.path() and relPath() methods work correctly
	mgi := rdt.classes["Guaranteed"].monGroups[mgName]
	if p := mgi.path("foo"); p != filepath.Join(mockFs.baseDir, "resctrl", "goresctrl.Guaranteed", "mon_groups", "goresctrl."+mgName, "foo") {
		t.Errorf("path() returned wrong path %q", p)
	}
	if p := mgi.relPath("foo"); p != filepath.Join("goresctrl.Guaranteed", "mon_groups", "goresctrl."+mgName, "foo") {
		t.Errorf("relPath() returned wrong path %q", p)
	}

	// Test deleting monitoring groups
	if err := cls.DeleteMonGroup(mgName); err != nil {
		t.Errorf("unexpected error when deleting mon group: %v", err)
	}
	if _, ok := cls.GetMonGroup("non-existing-group"); ok {
		t.Errorf("unexpected success when querying deleted group")
	}
	if _, err := os.Stat(mgPath); !os.IsNotExist(err) {
		t.Errorf("unexpected error when checking directory of deleted mon group: %v", err)
	}

	// Verify assigning pids to monitor group
	mgName = "test_group_2"
	mockFs.initMockMonGroup("Guaranteed", mgName)
	cls, _ = GetClass("Guaranteed")
	mg, _ = cls.CreateMonGroup(mgName, nil)

	pids = []string{"10"}
	if err := mg.AddPids(pids...); err != nil {
		t.Errorf("MonGroup.AddPids() failed: %v", err)
	}
	if p, err := mg.GetPids(); err != nil {
		t.Errorf("MonGroup.GetPids() failed: %v", err)
	} else if !cmp.Equal(p, pids) {
		t.Errorf("MonGroup.GetPids() returned %s, expected %s", p, pids)
	}
	mockFs.verifyTextFile(rdt.classes["Guaranteed"].monGroups[mgName].relPath("tasks"), "10\n")

	// Verify monitoring functionality
	expected := MonData{
		L3: MonL3Data{
			0: MonLeafData{
				"llc_occupancy":   1,
				"mbm_local_bytes": 2,
				"mbm_total_bytes": 3,
			},
			1: MonLeafData{
				"llc_occupancy":   11,
				"mbm_local_bytes": 12,
				"mbm_total_bytes": 13,
			},
			2: MonLeafData{
				"llc_occupancy":   21,
				"mbm_local_bytes": 22,
				"mbm_total_bytes": 23,
			},
			3: MonLeafData{
				"llc_occupancy":   31,
				"mbm_local_bytes": 32,
				"mbm_total_bytes": 33,
			},
		},
	}
	md := mg.GetMonData()
	if !cmp.Equal(md, expected) {
		t.Errorf("unexcpected monitoring data\nexpected:\n%s\nreceived:\n%s", utils.DumpJSON(expected), utils.DumpJSON(md))
	}
}

func TestBitMap(t *testing.T) {
	// Test ListStr()
	testSet := map[Bitmask]string{
		0x0:                "",
		0x1:                "0",
		0x2:                "1",
		0xf:                "0-3",
		0x555:              "0,2,4,6,8,10",
		0xaaa:              "1,3,5,7,9,11",
		0x1d1a:             "1,3-4,8,10-12",
		0xffffffffffffffff: "0-63",
	}
	for i, s := range testSet {
		// Test conversion to string
		listStr := i.ListStr()
		if listStr != s {
			t.Errorf("from %#x expected %q, got %q", i, s, listStr)
		}

		// Test conversion from string
		b, err := ListStrToBitmask(s)
		if err != nil {
			t.Errorf("unexpected err when converting %q: %v", s, err)
		}
		if b != i {
			t.Errorf("from %q expected %#x, got %#x", s, i, b)
		}
	}

	// Negative tests for ListStrToBitmask
	negTestSet := []string{
		",",
		"-",
		"1,",
		",12",
		"-4",
		"0-",
		"13-13",
		"14-13",
		"a-2",
		"b",
		"3-c",
		"64",
		"1,2,,3",
		"1,2,3-",
	}
	for _, s := range negTestSet {
		b, err := ListStrToBitmask(s)
		if err == nil {
			t.Errorf("expected err but got %#x when converting %q", b, s)
		}
	}
}

func TestListStrToArray(t *testing.T) {
	testSet := map[string][]int{
		"":              {},
		"0":             {0},
		"1":             {1},
		"0-3":           {0, 1, 2, 3},
		"4,2,0,6,10,8":  {0, 2, 4, 6, 8, 10},
		"1,3,5,7,9,11":  {1, 3, 5, 7, 9, 11},
		"1,3-4,10-12,8": {1, 3, 4, 8, 10, 11, 12},
	}
	for s, expected := range testSet {
		// Test conversion from string to list of integers
		a, err := listStrToArray(s)
		if err != nil {
			t.Errorf("unexpected error when converting %q: %v", s, err)
		}
		if !cmp.Equal(a, expected) {
			t.Errorf("from %q expected %v, got %v", s, expected, a)
		}
	}

	// Negative test cases
	negTestSet := []string{
		",",
		"-",
		"1,",
		",12",
		"-4",
		"0-",
		"13-13",
		"14-13",
		"a-2",
		"b",
		"3-c",
		"1,2,,3",
		"1,2,3-",
	}
	for _, s := range negTestSet {
		a, err := listStrToArray(s)
		if err == nil {
			t.Errorf("expected err but got %v when converting %q", a, s)
		}
	}
}
