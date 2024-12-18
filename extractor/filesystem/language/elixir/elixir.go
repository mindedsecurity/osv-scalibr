// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package elixir extracts packages from database.
package elixir

import (
	"bufio"
	"context"
	"path/filepath"
	"strings"

	"github.com/google/osv-scalibr/extractor"
	"github.com/google/osv-scalibr/extractor/filesystem"
	"github.com/google/osv-scalibr/extractor/filesystem/internal/units"
	"github.com/google/osv-scalibr/log"
	"github.com/google/osv-scalibr/plugin"
	"github.com/google/osv-scalibr/purl"
	"github.com/google/osv-scalibr/stats"
)

const (
	// Name is the unique name of this extractor.
	Name = "elixir/mixlock"

	// defaultMaxFileSizeBytes is the maximum file size this extractor will process.
	defaultMaxFileSizeBytes = 10 * units.MiB // 10 MB
)

// Config is the configuration for the Elixir extractor.
type Config struct {
	// Stats is a stats collector for reporting metrics.
	Stats stats.Collector
	// MaxFileSizeBytes is the maximum file size this extractor will unmarshal. If
	// `FileRequired` gets a bigger file, it will return false,
	MaxFileSizeBytes int64
}

// DefaultConfig returns the default configuration for the Elixir extractor.
func DefaultConfig() Config {
	return Config{
		MaxFileSizeBytes: defaultMaxFileSizeBytes,
	}
}

// Extractor structure for mix.lock files.
type Extractor struct {
	stats            stats.Collector
	maxFileSizeBytes int64
}

// New returns an Elixir extractor.
//
// For most use cases, initialize with:
// ```
// e := New(DefaultConfig())
// ```
func New(cfg Config) *Extractor {
	return &Extractor{
		stats:            cfg.Stats,
		maxFileSizeBytes: cfg.MaxFileSizeBytes,
	}
}

// Config returns the configuration of the extractor.
func (e Extractor) Config() Config {
	return Config{
		Stats:            e.stats,
		MaxFileSizeBytes: e.maxFileSizeBytes,
	}
}

// Name of the extractor.
func (e Extractor) Name() string { return Name }

// Version of the extractor.
func (e Extractor) Version() int { return 0 }

// Requirements of the extractor.
func (e Extractor) Requirements() *plugin.Capabilities { return &plugin.Capabilities{} }

// FileRequired returns true if the specified file matches the mix.lock pattern.
func (e Extractor) FileRequired(api filesystem.FileAPI) bool {
	path := api.Path()
	if !(filepath.Base(path) == "mix.lock") {
		return false
	}

	fileinfo, err := api.Stat()
	if err != nil || (e.maxFileSizeBytes > 0 && fileinfo.Size() > e.maxFileSizeBytes) {
		e.reportFileRequired(path, stats.FileRequiredResultSizeLimitExceeded)
		return false
	}

	e.reportFileRequired(path, stats.FileRequiredResultOK)
	return true
}

func (e Extractor) reportFileRequired(path string, result stats.FileRequiredResult) {
	if e.stats == nil {
		return
	}
	e.stats.AfterFileRequired(e.Name(), &stats.FileRequiredStats{
		Path:   path,
		Result: result,
	})
}

// Extract parses the mix.lock file to extract Elixir package dependencies.
func (e Extractor) Extract(ctx context.Context, input *filesystem.ScanInput) ([]*extractor.Inventory, error) {
	packages, err := e.extractFromInput(ctx, input)
	if e.stats != nil {
		var fileSizeBytes int64
		if input.Info != nil {
			fileSizeBytes = input.Info.Size()
		}
		e.stats.AfterFileExtracted(e.Name(), &stats.FileExtractedStats{
			Path:          input.Path,
			Result:        filesystem.ExtractorErrorToFileExtractedResult(err),
			FileSizeBytes: fileSizeBytes,
		})
	}
	return packages, err
}

func (e Extractor) extractFromInput(ctx context.Context, input *filesystem.ScanInput) ([]*extractor.Inventory, error) {

	var pkgs []*extractor.Inventory
	reader := bufio.NewScanner(input.Reader)

	lineNum := 0
	for reader.Scan() {
		lineNum++
		line := strings.TrimSpace(reader.Text())

		if line == "" || strings.HasPrefix(line, "#") || line == "%{" || line == "}" {
			continue
		}

		name, version := parseMixLockLine(line)

		if name == "" || version == "" {
			log.Warnf("Missing name or version on line %d: %s", lineNum, line)
			continue
		}

		i := &extractor.Inventory{
			Name:      name,
			Version:   version,
			Locations: []string{input.Path},
		}
		pkgs = append(pkgs, i)
	}

	if err := reader.Err(); err != nil {
		log.Errorf("Error reading mix.lock: %v", err)
		return []*extractor.Inventory{}, err
	}

	return pkgs, nil
}

// parseMixLockLine extracts the package name and version from a single mix.lock line.
func parseMixLockLine(line string) (name, version string) {
	if !strings.Contains(line, ":hex") {
		return "", "" // Early exit for non-hex lines
	}

	parts := strings.SplitN(line, ": ", 2)
	if len(parts) < 2 {
		return "", "" // Invalid line structure
	}

	name = strings.Trim(parts[0], `"`)

	valueParts := strings.Split(parts[1], ",")
	if len(valueParts) < 3 {
		return "", "" // Not enough fields in value
	}

	version = strings.Trim(valueParts[2], ` "`) // Clean up version field
	return name, version
}

// ToPURL converts an inventory created by this extractor into a PURL.
func (e Extractor) ToPURL(i *extractor.Inventory) *purl.PackageURL {
	return &purl.PackageURL{
		Type:    purl.TypeHex,
		Name:    strings.ToLower(i.Name),
		Version: i.Version,
	}
}

// Ecosystem returns the OSV Ecosystem of the software extracted by this extractor.
func (Extractor) Ecosystem(i *extractor.Inventory) string {
	return "Hex"
}
