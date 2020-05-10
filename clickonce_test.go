// Copyright 2020 Stefano Cotta Ramusino. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clickonce

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"
)

const (
	applicationUrl = "https://aka.ms/resxunusedfinder"
)

var subset = []string{"ResxUnusedFinder.exe"}

var co ClickOnce

func TestClickOnce_SetLogger(t *testing.T) {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	co.SetLogger(logger)
}

func TestClickOnce_Init(t *testing.T) {
	err := co.Init(applicationUrl)
	if err != nil {
		t.Error(err)
	}
}

func TestClickOnce_GetAll(t *testing.T) {
	err := co.GetAll()
	if err != nil {
		t.Error(err)
	}
}

func TestClickOnce_DeployedFiles(t *testing.T) {
	deployedFiles := co.DeployedFiles()
	for _, subsetFileName := range subset {
		if _, ok := deployedFiles[subsetFileName]; !ok {
			t.Errorf("'%s' not found in DeployedFiles()", subsetFileName)
		}
	}
}

func TestClickOnce_Get(t *testing.T) {
	tempOutputDir, err := ioutil.TempDir("", "clickonce_test_")
	if err != nil {
		t.Error("cannot create temporary directory for test")
	}

	defer func() {
		err = os.RemoveAll(tempOutputDir)
		if err != nil {
			t.Error("cannot delete temporary directory used for test")
		}
	}()

	co.SetOutputDir(tempOutputDir)

	err = co.Get(subset)
	if err != nil {
		t.Error(err)
	}

	found := 0
	err = filepath.Walk(tempOutputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		for _, s := range subset {
			if !info.IsDir() && info.Name() == s {
				found += 1
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
	if found != len(subset) {
		t.Error("missing downloaded files")
	}
}
