package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"golang.org/x/mod/semver"
)

type Config []string

type Result struct {
	BazelVersion   string `json:"bazel_version"`
	GrpcVersion    string `json:"grpc_version"`
	ProtoVersion   string `json:"proto_version"`
	RulesGoVersion string `json:"rules_go_version"`
	RulesCcVersion string `json:"rules_cc_version"`
	Success        bool   `json:"success"`
	Date           string `json:"date"`
	LogFile        string `json:"log_file,omitempty"`
}

func main() {
	fmt.Println("Starting gRPC Version Matrix Testing Tool")

	bazelFlag := flag.String("bazel", "7,8,9", "Comma-separated list of Bazel versions to test")
	grpcFlag := flag.String("grpc", "", "Specific gRPC version to test (defaults to latest)")
	protoFlag := flag.String("proto", "", "Specific Protobuf version to test (defaults to latest)")
	rulesGoFlag := flag.String("rules_go", "", "Specific rules_go version to test (defaults to latest)")
	rulesCcFlag := flag.String("rules_cc", "", "Specific rules_cc version to test (defaults to latest)")
	flag.Parse()

	bazelVersions := strings.Split(*bazelFlag, ",")
	for i := range bazelVersions {
		bazelVersions[i] = strings.TrimSpace(bazelVersions[i])
	}

	fmt.Printf("Configured Bazel versions: %v\n", bazelVersions)

	// Discover versions
	latestBazelVersions, err := discoverBazelVersions(bazelVersions)
	if err != nil {
		fmt.Printf("Error discovering Bazel versions: %v\n", err)
		return
	}
	fmt.Printf("Discovered Bazel versions: %v\n", latestBazelVersions)

	var latestGrpc string
	if *grpcFlag != "" {
		latestGrpc = *grpcFlag
	} else {
		latestGrpc, err = discoverBCRVersion("grpc")
		if err != nil {
			fmt.Printf("Error discovering gRPC version: %v\n", err)
			return
		}
	}
	fmt.Printf("Using gRPC version: %s\n", latestGrpc)

	var latestProto string
	if *protoFlag != "" {
		latestProto = *protoFlag
	} else {
		latestProto, err = discoverBCRVersion("protobuf")
		if err != nil {
			fmt.Printf("Error discovering Protobuf version: %v\n", err)
			return
		}
	}
	fmt.Printf("Using Protobuf version: %s\n", latestProto)

	var latestRulesGo string
	if *rulesGoFlag != "" {
		latestRulesGo = *rulesGoFlag
	} else {
		latestRulesGo, err = discoverBCRVersion("rules_go")
		if err != nil {
			fmt.Printf("Error discovering rules_go version: %v\n", err)
			return
		}
	}
	fmt.Printf("Using rules_go version: %s\n", latestRulesGo)

	var latestRulesCc string
	if *rulesCcFlag != "" {
		latestRulesCc = *rulesCcFlag
	} else {
		latestRulesCc, err = discoverBCRVersion("rules_cc")
		if err != nil {
			fmt.Printf("Error discovering rules_cc version: %v\n", err)
			return
		}
	}
	fmt.Printf("Using rules_cc version: %s\n", latestRulesCc)

	// 3. Load existing results
	results, err := loadResults("results.json")
	if err != nil {
		fmt.Printf("Error loading results: %v. Starting fresh.\n", err)
		results = []Result{}
	}

	// 4. Run tests
	updated := false
	for _, bazelVer := range latestBazelVersions {
		if alreadyTested(results, bazelVer, latestGrpc, latestProto, latestRulesGo, latestRulesCc) {
			fmt.Printf("Already tested: Bazel %s, gRPC %s, Proto %s, rules_go %s, rules_cc %s. Skipping.\n", bazelVer, latestGrpc, latestProto, latestRulesGo, latestRulesCc)
			continue
		}

		fmt.Printf("Testing: Bazel %s, gRPC %s, Proto %s, rules_go %s, rules_cc %s\n", bazelVer, latestGrpc, latestProto, latestRulesGo, latestRulesCc)
		success, logPath, err := runTest(bazelVer, latestGrpc, latestProto, latestRulesGo, latestRulesCc)
		if err != nil {
			fmt.Printf("Error running test: %v\n", err)
			continue
		}

		results = append(results, Result{
			BazelVersion:   bazelVer,
			GrpcVersion:    latestGrpc,
			ProtoVersion:   latestProto,
			RulesGoVersion: latestRulesGo,
			RulesCcVersion: latestRulesCc,
			Success:        success,
			Date:           time.Now().Format("2006-01-02"),
			LogFile:        logPath,
		})
		updated = true
	}

	// 5. Save results and generate report
	sort.Slice(results, func(i, j int) bool {
		normalize := func(v string) string {
			v = strings.Replace(v, ".bcr.", "-bcr.", 1)
			parts := strings.Split(v, ".")
			if len(parts) == 1 {
				v += ".0.0"
			} else if len(parts) == 2 {
				v += ".0"
			}
			return "v" + v
		}

		vi := normalize(results[i].BazelVersion)
		vj := normalize(results[j].BazelVersion)
		if results[i].BazelVersion != results[j].BazelVersion {
			return semver.Compare(vi, vj) < 0
		}
		gi := normalize(results[i].GrpcVersion)
		gj := normalize(results[j].GrpcVersion)
		if results[i].GrpcVersion != results[j].GrpcVersion {
			return semver.Compare(gi, gj) < 0
		}
		pi := normalize(results[i].ProtoVersion)
		pj := normalize(results[j].ProtoVersion)
		return semver.Compare(pi, pj) < 0
	})

	err = saveResults("results.json", results)
	if err != nil {
		fmt.Printf("Error saving results: %v\n", err)
	}

	err = generateReport("results.md", results)
	if err != nil {
		fmt.Printf("Error generating report: %v\n", err)
	}

	if !updated {
		fmt.Println("No new combinations tested (but files updated with sorted results).")
	}
}



func discoverBazelVersions(config Config) (map[string]string, error) {
	result := make(map[string]string)

	if len(config) == 0 {
		// Existing logic for empty config (latest stable)
		url := "https://api.github.com/repos/bazelbuild/bazel/releases"
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		token := os.Getenv("GITHUB_TOKEN")
		if token != "" {
			req.Header.Set("Authorization", "token "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		}

		var releases []struct {
			TagName    string `json:"tag_name"`
			Prerelease bool   `json:"prerelease"`
		}
		err = json.NewDecoder(resp.Body).Decode(&releases)
		if err != nil {
			return nil, err
		}

		for _, r := range releases {
			if !r.Prerelease {
				v := r.TagName
				if !strings.HasPrefix(v, "v") {
					v = "v" + v
				}
				if semver.IsValid(v) {
					result["latest"] = r.TagName
					return result, nil
				}
			}
		}
		return nil, fmt.Errorf("no stable Bazel release found")
	}

	highest := make(map[string]string)
	for _, major := range config {
		highest[major] = ""
	}

	page := 1
	for {
		url := fmt.Sprintf("https://api.github.com/repos/bazelbuild/bazel/releases?per_page=100&page=%d", page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		token := os.Getenv("GITHUB_TOKEN")
		if token != "" {
			req.Header.Set("Authorization", "token "+token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		}

		var releases []struct {
			TagName    string `json:"tag_name"`
			Prerelease bool   `json:"prerelease"`
		}

		err = json.NewDecoder(resp.Body).Decode(&releases)
		resp.Body.Close() // Close body after decoding
		if err != nil {
			return nil, err
		}

		if len(releases) == 0 {
			break
		}

		for _, r := range releases {
			if r.Prerelease {
				continue
			}
			tag := r.TagName
			v := tag
			if !strings.HasPrefix(v, "v") {
				v = "v" + v
			}
			if !semver.IsValid(v) {
				continue
			}

			for _, major := range config {
				if tag == major || tag == "v"+major || strings.HasPrefix(tag, major+".") || strings.HasPrefix(tag, "v"+major+".") {
					currentHighest := highest[major]
					if currentHighest == "" || semver.Compare(v, currentHighest) > 0 {
						highest[major] = v
					}
				}
			}
		}

		page++
		if page > 5 { // Limit to 5 pages
			break
		}
	}

	for _, major := range config {
		if highest[major] != "" {
			result[major] = strings.TrimPrefix(highest[major], "v")
		}
	}

	return result, nil
}

func discoverBCRVersion(module string) (string, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/bazelbuild/bazel-central-registry/main/modules/%s/metadata.json", module)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("BCR returned status %d", resp.StatusCode)
	}

	var metadata struct {
		Versions []string `json:"versions"`
	}

	err = json.NewDecoder(resp.Body).Decode(&metadata)
	if err != nil {
		return "", err
	}

	if len(metadata.Versions) == 0 {
		return "", fmt.Errorf("no versions found for module %s", module)
	}

	highest := ""
	for _, v := range metadata.Versions {
		sv := "v" + v
		if !semver.IsValid(sv) {
			continue
		}
		if highest == "" || semver.Compare(sv, highest) > 0 {
			highest = sv
		}
	}

	if highest == "" {
		return "", fmt.Errorf("no valid semantic versions found for module %s", module)
	}

	return strings.TrimPrefix(highest, "v"), nil
}

func loadResults(filename string) ([]Result, error) {
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return []Result{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var results []Result
	err = json.NewDecoder(file).Decode(&results)
	return results, err
}

func saveResults(filename string, results []Result) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}

func alreadyTested(results []Result, bazel, grpc, proto, rulesgo, rulescc string) bool {
	for _, r := range results {
		if r.BazelVersion == bazel && r.GrpcVersion == grpc && r.ProtoVersion == proto && r.RulesGoVersion == rulesgo && r.RulesCcVersion == rulescc {
			return true
		}
	}
	return false
}

func runTest(bazelVersion, grpcVersion, protoVersion, rulesGoVersion, rulesCcVersion string) (bool, string, error) {
	tmpDir, err := os.MkdirTemp("", "bazel_test_")
	if err != nil {
		return false, "", err
	}
	defer os.RemoveAll(tmpDir)

	tmplPath := filepath.Join(".", "templates", "MODULE.bazel.tmpl")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		return false, "", err
	}

	moduleFile, err := os.Create(filepath.Join(tmpDir, "MODULE.bazel"))
	if err != nil {
		return false, "", err
	}
	defer moduleFile.Close()

	data := struct {
		GrpcVersion     string
		ProtobufVersion string
		RulesGoVersion  string
		RulesCcVersion  string
	}{
		GrpcVersion:     grpcVersion,
		ProtobufVersion: protoVersion,
		RulesGoVersion:  rulesGoVersion,
		RulesCcVersion:  rulesCcVersion,
	}

	err = tmpl.Execute(moduleFile, data)
	if err != nil {
		return false, "", err
	}

	err = copyDir(filepath.Join(".", "proto"), filepath.Join(tmpDir, "proto"))
	if err != nil {
		return false, "", err
	}

	// We need to use bazelisk or assume bazel is in path and respects USE_BAZEL_VERSION
	cmd := exec.Command("bazel", "build", "//...")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "USE_BAZEL_VERSION="+bazelVersion)

	output, err := cmd.CombinedOutput()
	fmt.Printf("Bazel output for %s:\n%s\n", bazelVersion, string(output))

	// Create logs directory
	logsDir := "logs"
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return false, "", err
	}

	// Generate log file name
	safeRulesGo := rulesGoVersion
	if safeRulesGo == "" {
		safeRulesGo = "none"
	}
	safeRulesCc := rulesCcVersion
	if safeRulesCc == "" {
		safeRulesCc = "none"
	}
	logFileName := fmt.Sprintf("bazel-%s-grpc-%s-proto-%s-rulesgo-%s-rulescc-%s.log", bazelVersion, grpcVersion, protoVersion, safeRulesGo, safeRulesCc)
	logFilePath := filepath.Join(logsDir, logFileName)

	// Write log file
	if err := os.WriteFile(logFilePath, output, 0644); err != nil {
		return false, "", err
	}

	if err != nil {
		return false, logFilePath, nil
	}

	return true, logFilePath, nil
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}

func generateReport(filename string, results []Result) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintln(file, "# gRPC Bazel Compatibility Results")
	fmt.Fprintln(file, "")
	fmt.Fprintln(file, "| Bazel Version | gRPC Version | Protobuf Version | rules_go Version | rules_cc Version | Status | Date |")
	fmt.Fprintln(file, "| --- | --- | --- | --- | --- | --- | --- |")

	for _, r := range results {
		status := "✅ OK"
		if !r.Success {
			status = "❌ Failed"
		}
		
		statusStr := status
		if r.LogFile != "" {
			statusStr = fmt.Sprintf("[%s](%s)", status, r.LogFile)
		}

		fmt.Fprintf(file, "| %s | %s | %s | %s | %s | %s | %s |\n", r.BazelVersion, r.GrpcVersion, r.ProtoVersion, r.RulesGoVersion, r.RulesCcVersion, statusStr, r.Date)
	}

	return nil
}
