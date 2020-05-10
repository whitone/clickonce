// Copyright 2020 Stefano Cotta Ramusino. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package clickonce provides support for downloading
// ClickOnce applications.
package clickonce

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	// Common text encodings for HTML documents
	"golang.org/x/net/html/charset"
)

type coType int

const (
	// AssemblyDependency indicates a dependency required for the application.
	AssemblyDependency coType = iota

	// NonAssemblyFile indicates a file used by the application.
	NonAssemblyFile
)

// A DeployedFile represents a ClickOnce deployed file.
type DeployedFile struct {
	// Deployed file type.
	Type coType
	// Deployed file content.
	Content []byte
}

// A ClickOnce represents ClickOnce application files.
type ClickOnce struct {
	deployedFiles map[string]DeployedFile
	subset        map[string]bool
	baseUrl       *url.URL
	assembly      *coAssembly
	outputDir     string
	noSuffix      bool
	offline       bool
	notFound      int
	logger        *log.Logger
}

type remoteFile struct {
	url       *url.URL
	size      int
	digest    string
	algorithm string
}

// Following structs are generated with xmljson2struct (github.com/rai-project/xj2s)

type coAssembly struct {
	File              []coFile              `xml:"file"`
	DependentAssembly []coDependentAssembly `xml:"dependency>dependentAssembly"`
}

type coBase struct {
	Size string `xml:"size,attr"`
	Hash coHash `xml:"hash"`
}

type coFile struct {
	coBase
	Name string `xml:"name,attr"`
}

type coDependentAssembly struct {
	coBase
	Codebase            string `xml:"codebase,attr"`
	DependencyType      string `xml:"dependencyType,attr"`
	AllowDelayedBinding string `xml:"allowDelayedBinding,attr"`
}

type coHash struct {
	DigestMethod coDigestMethod `xml:"DigestMethod"`
	DigestValue  string         `xml:"DigestValue"`
}

type coDigestMethod struct {
	Algorithm string `xml:"Algorithm,attr"`
}

const (
	manifestExtension     = ".manifest"
	deployedFileExtension = ".deploy"
)

// Init with a valid ClickOnce deployment manifest URL.
func (co *ClickOnce) Init(appUrl string) error {
	if co.logger == nil {
		co.SetLogger(nil)
	}

	if appUrl == "" {
		return errors.New("missing valid application URL")
	}

	req, err := http.NewRequest("GET", appUrl, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode == http.StatusNotFound {
		return errors.New("no application available at '" + appUrl + "'")
	}

	appContent, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}

	err = res.Body.Close()
	if err != nil {
		return err
	}

	if len(appContent) == 0 {
		return errors.New("application file is empty")
	}

	co.assembly, err = decodeManifest(appContent)
	if err != nil {
		return err
	}

	co.baseUrl = res.Request.URL

	return nil
}

// SetOutputDir set the path of the directory where to save deployed files.
func (co *ClickOnce) SetOutputDir(outputDir string) {
	co.outputDir = outputDir
}

// SetLogger set logger for the library.
func (co *ClickOnce) SetLogger(logger *log.Logger) {
	if logger == nil {
		logger = log.New(ioutil.Discard, "", 0)
	}
	co.logger = logger
}

// DeployedFiles get all deployed files.
func (co *ClickOnce) DeployedFiles() map[string]DeployedFile {
	return co.deployedFiles
}

// GetAll download all files required or used by ClickOnce application.
func (co *ClickOnce) GetAll() error {
	err := co.Get(nil)
	if err != nil {
		return err
	}

	co.offline = true

	return nil
}

// initSubset init subset.
func (co *ClickOnce) initSubset(subset []string) error {
	co.subset = make(map[string]bool, len(subset))
	for _, s := range subset {
		if s == "" {
			return errors.New("empty filename is not valid in a subset")
		}
		co.subset[s] = false
	}
	return nil
}

// checkDownload verifies that all elements in subset are already downloaded.
func (co *ClickOnce) checkDownload() {
	for deployedFilePath := range co.deployedFiles {
		codebase := strings.Replace(deployedFilePath, "\\", "/", -1)
		deployedFileName := path.Base(codebase)
		if _, ok := co.subset[deployedFileName]; ok {
			co.subset[deployedFileName] = true
		}
	}
}

// findSubset checks if all element in subset are available.
func (co *ClickOnce) findSubset(subset []string) error {
	for _, s := range subset {
		if !co.subset[s] {
			co.logger.Printf("Requested file '%s' not found\n", s)
			co.notFound += 1
		}
	}

	if co.notFound == len(subset) {
		return errors.New("none of requested files are found")
	}

	return nil
}

// Get download only a subset of all files required or used by ClickOnce application.
// If subset is nil or empty, all files are downloaded.
func (co *ClickOnce) Get(subset []string) error {
	if co.baseUrl == nil || co.assembly == nil {
		return errors.New("clickonce not initialized")
	}

	validSubset := subset != nil && len(subset) != 0

	if validSubset {
		err := co.initSubset(subset)
		if err != nil {
			return err
		}
	}

	if !co.offline {
		err := co.retrieveAllDeployedFiles(co.assembly)
		if err != nil {
			return err
		}
	} else {
		co.logger.Println("Files already offline, download skipped")
		if validSubset {
			co.checkDownload()
		}
	}

	co.notFound = 0

	if validSubset {
		err := co.findSubset(subset)
		if err != nil {
			return err
		}
	}

	if co.outputDir != "" {
		err := co.saveAllDeployedFiles()
		if err != nil {
			return err
		}
	}

	if validSubset && co.notFound != 0 {
		return errors.New("not all or requested files are found")
	}

	return nil
}

// remoteFileInfo extract remote file informations from a ClickOnce manifest.
func (co *ClickOnce) remoteFileInfo(deployedFileUrl *url.URL, deployedFile coBase) (*remoteFile, error) {
	size, err := strconv.Atoi(deployedFile.Size)
	if err != nil {
		return nil, err
	}

	alg, err := url.Parse(deployedFile.Hash.DigestMethod.Algorithm)
	if err != nil {
		return nil, err
	}
	algorithm := strings.ToLower(alg.Fragment)

	digest := deployedFile.Hash.DigestValue

	return &remoteFile{deployedFileUrl, size, digest, algorithm}, nil
}

// retrieveDeployedFile get a deployed file of a ClickOnce application if it's in the requested subset.
func (co *ClickOnce) retrieveDeployedFile(deployedFilePath string, deployedFile coBase, deployedFileType coType) error {
	if deployedFilePath == "" {
		co.logger.Println("Missing valid path, skipped")
		return nil
	}

	deployedFilePosixPath := strings.Replace(deployedFilePath, "\\", "/", -1)
	deployedFileName := path.Base(deployedFilePosixPath)

	manifest := isManifest(deployedFileName)

	if co.subset != nil && !manifest {
		if _, ok := co.subset[deployedFileName]; !ok {
			co.logger.Printf("'%s' not in requested subset, skipped\n", deployedFileName)
			return nil
		}
	}

	if _, ok := co.deployedFiles[deployedFilePath]; ok {
		co.logger.Printf("'%s' already downloaded, skipped\n", deployedFilePath)
		return nil
	}

	deployedFileUrl, err := co.baseUrl.Parse(deployedFilePosixPath)
	if err != nil {
		return err
	}

	if deployedFileType == AssemblyDependency && manifest {
		co.baseUrl = deployedFileUrl
	}

	remoteFile, err := co.remoteFileInfo(deployedFileUrl, deployedFile)
	if err != nil {
		return err
	}

	deployedFileContent, suffix, err := downloadAndCheck(remoteFile, !co.noSuffix, co.logger)
	if err != nil {
		return err
	}

	co.noSuffix = !suffix

	if co.deployedFiles == nil {
		co.deployedFiles = make(map[string]DeployedFile)
	}

	co.logger.Printf("Added '%s' to deployedFiles\n", deployedFilePath)

	co.deployedFiles[deployedFilePath] = DeployedFile{
		Type:    deployedFileType,
		Content: deployedFileContent,
	}

	if co.subset != nil {
		co.subset[deployedFileName] = true
	}

	if manifest {
		err = co.subApplication(deployedFileContent)
		if err != nil {
			return err
		}
	}

	return nil
}

// subApplication decode a manifest and retrieve all deployed files related to a sub application.
func (co *ClickOnce) subApplication(manifestContent []byte) (err error) {
	assembly, err := decodeManifest(manifestContent)
	if err != nil {
		return
	}

	err = co.retrieveAllDeployedFiles(assembly)
	if err != nil {
		return
	}

	return
}

// retrieveAllDeployedFiles get all wanted deployed files of a ClickOnce application.
func (co *ClickOnce) retrieveAllDeployedFiles(assembly *coAssembly) error {
	for _, dependentAssembly := range assembly.DependentAssembly {
		if dependentAssembly.DependencyType != "install" {
			co.logger.Println("Found dependency not to install, skipped")
			continue
		}
		err := co.retrieveDeployedFile(dependentAssembly.Codebase, dependentAssembly.coBase, AssemblyDependency)
		if err != nil {
			return nil
		}
	}

	for _, f := range assembly.File {
		err := co.retrieveDeployedFile(f.Name, f.coBase, NonAssemblyFile)
		if err != nil {
			return nil
		}
	}

	return nil
}

// saveAllDeployedFile saves a deployed file of a ClickOnce application if it's in the requested subset.
func (co *ClickOnce) saveDeployedFile(deployedFilePath string, deployedFileContent []byte) error {
	codebase := strings.Replace(deployedFilePath, "\\", "/", -1)
	deployedFileName := path.Base(codebase)
	if co.subset != nil {
		if _, ok := co.subset[deployedFileName]; !ok {
			co.logger.Printf("'%s' not in requested subset, skipped\n", deployedFileName)
			return nil
		}
	}

	err := os.MkdirAll(path.Dir(path.Join(co.outputDir, codebase)), os.ModePerm)
	if err != nil {
		return err
	}

	co.logger.Println("Saving " + path.Join(co.outputDir, codebase))

	err = ioutil.WriteFile(path.Join(co.outputDir, codebase), deployedFileContent, 0644)
	if err != nil {
		return err
	}

	co.logger.Println("Saved " + path.Join(co.outputDir, codebase))
	return nil
}

// saveAllDeployedFiles saves all wanted deployed files of a ClickOnce application.
func (co *ClickOnce) saveAllDeployedFiles() error {
	for deployedFilePath, deployedFile := range co.deployedFiles {
		err := co.saveDeployedFile(deployedFilePath, deployedFile.Content)
		if err != nil {
			return err
		}
	}
	return nil
}

// isManifest check if a file is a ClickOnce application manifest.
func isManifest(filename string) bool {
	return path.Ext(filename) == manifestExtension
}

// decodeManifest decodes a ClickOnce manifest.
func decodeManifest(data []byte) (*coAssembly, error) {
	dec := xml.NewDecoder(bytes.NewBuffer(data))
	dec.CharsetReader = charset.NewReaderLabel
	dec.Strict = false

	var assembly coAssembly
	if err := dec.Decode(&assembly); err != nil {
		return nil, err
	}

	return &assembly, nil
}

// download returns an HTTP response from a URL.
func download(url string) (res *http.Response, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return
	}

	res, err = http.DefaultClient.Do(req)
	if err != nil {
		return
	}

	return
}

// downloadAndCheck get a remote file and check size and checksum.
func downloadAndCheck(file *remoteFile, suffix bool, logger *log.Logger) ([]byte, bool, error) {
	filename := filepath.Base(file.url.Path)

	if file.algorithm != "sha1" && file.algorithm != "sha256" {
		return nil, suffix, errors.New(file.algorithm + " digest algorithm not supported for '" + filename + "'")
	}

	downloadUrl := file.url.String()

	if suffix && !isManifest(filepath.Base(file.url.Path)) {
		downloadUrl += deployedFileExtension
	}

	logger.Printf("Downloading '%s' from '%s'\n", filename, downloadUrl)

	res, err := download(downloadUrl)
	if err != nil {
		return nil, suffix, err
	}

	if res.StatusCode == http.StatusNotFound {
		// Check if build with MapFileExtensions as false

		downloadUrl := strings.TrimSuffix(downloadUrl, path.Ext(downloadUrl))

		logger.Printf("Not found, trying to download '%s' from '%s'\n", filename, downloadUrl)

		res, err := download(downloadUrl)
		if err != nil {
			return nil, suffix, err
		}

		if res.StatusCode == http.StatusNotFound {
			return nil, suffix, errors.New("no file available at '" + downloadUrl + "'")
		}

		logger.Printf("Application files deployed without default '%s' suffix", deployedFileExtension)
		suffix = false
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, suffix, err
	}

	err = res.Body.Close()
	if err != nil {
		return nil, suffix, err
	}

	if file.size != len(body) {
		return nil, suffix, fmt.Errorf("size mismatch for file '%s': expected %d, got %d",
			filename, file.size, len(body))
	}

	var checksum []byte

	if file.algorithm == "sha1" {
		checksumSHA1 := sha1.Sum(body)
		checksum = checksumSHA1[:]
	} else {
		checksumSHA256 := sha256.Sum256(body)
		checksum = checksumSHA256[:]
	}

	if file.digest != base64.StdEncoding.EncodeToString(checksum) {
		return nil, suffix, fmt.Errorf("digest mismatch for file '%s'", filename)
	}

	logger.Printf("Downloaded '%s'\n", filename)

	return body, suffix, nil
}
