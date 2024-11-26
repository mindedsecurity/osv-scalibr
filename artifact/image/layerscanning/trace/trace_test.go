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

package trace

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/osv-scalibr/artifact/image"
	"github.com/google/osv-scalibr/artifact/image/layerscanning/testing/fakechainlayer"
	"github.com/google/osv-scalibr/artifact/image/layerscanning/testing/fakelayer"
	"github.com/google/osv-scalibr/extractor"
	"github.com/google/osv-scalibr/extractor/filesystem"
	"github.com/google/osv-scalibr/stats"
	"github.com/google/osv-scalibr/testing/fakeextractor"
)

func TestPopulateLayerDetails(t *testing.T) {
	fakeExtractor := fakeextractor.New("fake-extractor", 1, []string{"foo"}, nil)

	tests := []struct {
		name          string
		inventory     []*extractor.Inventory
		originDetails map[extractor.InventoryKey]*extractor.LayerDetails
		want          []*extractor.Inventory
	}{
		{
			name:          "empty inventory",
			inventory:     []*extractor.Inventory{},
			originDetails: map[extractor.InventoryKey]*extractor.LayerDetails{},
			want:          []*extractor.Inventory{},
		},
		{
			name: "inventory with no origin details",
			inventory: []*extractor.Inventory{
				{
					Name:      "foo",
					Version:   "1.0",
					Locations: []string{"/foo"},
					Extractor: fakeExtractor,
				},
			},
			originDetails: map[extractor.InventoryKey]*extractor.LayerDetails{},
			want: []*extractor.Inventory{
				{
					Name:      "foo",
					Version:   "1.0",
					Locations: []string{"/foo"},
					Extractor: fakeExtractor,
				},
			},
		},
		{
			name: "no matching origin details",
			inventory: []*extractor.Inventory{
				{
					Name:      "foo",
					Version:   "1.0",
					Locations: []string{"/foo"},
					Extractor: fakeExtractor,
				},
			},
			originDetails: map[extractor.InventoryKey]*extractor.LayerDetails{
				extractor.InventoryKey{PURL: "pkg:pypi/foo@1.0", Path: "/bar"}: &extractor.LayerDetails{
					Index:       1,
					DiffID:      "diff-id-1",
					Command:     "command-1",
					InBaseImage: false,
				},
			},
			want: []*extractor.Inventory{
				{
					Name:      "foo",
					Version:   "1.0",
					Locations: []string{"/foo"},
					Extractor: fakeExtractor,
				},
			},
		},
		{
			name: "matching origin details",
			inventory: []*extractor.Inventory{
				{
					Name:      "foo",
					Version:   "1.0",
					Locations: []string{"/foo"},
					Extractor: fakeExtractor,
				},
			},
			originDetails: map[extractor.InventoryKey]*extractor.LayerDetails{
				extractor.InventoryKey{PURL: "pkg:pypi/foo@1.0", Path: "/foo"}: &extractor.LayerDetails{
					Index:       1,
					DiffID:      "diff-id-1",
					Command:     "command-1",
					InBaseImage: false,
				},
			},
			want: []*extractor.Inventory{
				{
					Name:      "foo",
					Version:   "1.0",
					Locations: []string{"/foo"},
					Extractor: fakeExtractor,
					LayerDetails: &extractor.LayerDetails{
						Index:       1,
						DiffID:      "diff-id-1",
						Command:     "command-1",
						InBaseImage: false,
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PopulateLayerDetails(tc.inventory, tc.originDetails)
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreFields(extractor.Inventory{}, "Extractor")); diff != "" {
				t.Errorf("PopulateLayerDetails(%v, %v) returned an unexpected diff (-want +got): %v", tc.inventory, tc.originDetails, diff)
			}
		})
	}
}

func setupFakeChainLayer(t *testing.T, testDir string, index int, diffID string, command string, fileContents map[string]string) *fakechainlayer.FakeChainLayer {
	t.Helper()

	layer := fakelayer.New(diffID, command)
	chainLayer, err := fakechainlayer.New(testDir, index, diffID, command, layer, fileContents)
	if err != nil {
		t.Fatalf("fakechainlayer.New(%d, %q, %q, %v, %v) failed: %v", index, diffID, command, layer, fileContents, err)
	}
	return chainLayer
}

func TestResolveOriginLayer(t *testing.T) {
	const (
		// Fake file names used in tests.
		fooFile = "foo.txt"
		barFile = "bar.txt"
		bazFile = "baz.txt"

		// Fake package names used in tests.
		fooPackage = "foo"
		barPackage = "bar"
		bazPackage = "baz"
	)

	// Chain Layer 1: Start with foo and bar packages.
	// - foo.txt
	// - bar.txt
	fakeChainLayer1 := setupFakeChainLayer(t, t.TempDir(), 0, "diff-id-1", "command-1", map[string]string{
		fooFile: fooPackage,
		barFile: barPackage,
	})
	fakeExtractor1 := fakeextractor.New("fake-extractor-1", 1, []string{fooFile, barFile}, map[string]fakeextractor.NamesErr{
		fooFile: fakeextractor.NamesErr{
			Names: []string{fooPackage},
		},
		barFile: fakeextractor.NamesErr{
			Names: []string{barPackage},
		},
	})

	// Chain Layer 2: Deletes bar package.
	// - foo.txt
	fakeChainLayer2 := setupFakeChainLayer(t, t.TempDir(), 1, "diff-id-2", "command-2", map[string]string{
		fooFile: fooPackage,
	})
	fakeExtractor2 := fakeextractor.New("fake-extractor-2", 1, []string{fooFile}, map[string]fakeextractor.NamesErr{
		fooFile: fakeextractor.NamesErr{
			Names: []string{fooPackage},
		},
	})

	// Chain Layer 3: Adds baz package.
	// - foo.txt
	// - baz.txt
	fakeChainLayer3 := setupFakeChainLayer(t, t.TempDir(), 2, "diff-id-3", "command-3", map[string]string{
		fooFile: fooPackage,
		bazFile: bazPackage,
	})
	fakeExtractor3 := fakeextractor.New("fake-extractor-3", 1, []string{fooFile, bazFile}, map[string]fakeextractor.NamesErr{
		fooFile: fakeextractor.NamesErr{
			Names: []string{fooPackage},
		},
		bazFile: fakeextractor.NamesErr{
			Names: []string{bazPackage},
		},
	})

	// Chain Layer 4: Adds bar package back.
	// - foo.txt
	// - bar.txt
	// - baz.txt
	fakeChainLayer4 := setupFakeChainLayer(t, t.TempDir(), 3, "diff-id-4", "command-4", map[string]string{
		fooFile: fooPackage,
		barFile: barPackage,
		bazFile: bazPackage,
	})
	fakeExtractor4 := fakeextractor.New("fake-extractor-4", 1, []string{fooFile, barFile, bazFile}, map[string]fakeextractor.NamesErr{
		fooFile: fakeextractor.NamesErr{
			Names: []string{fooPackage},
		},
		barFile: fakeextractor.NamesErr{
			Names: []string{barPackage},
		},
		bazFile: fakeextractor.NamesErr{
			Names: []string{bazPackage},
		},
	})

	tests := []struct {
		name             string
		inventory        []*extractor.Inventory
		extractor        filesystem.Extractor
		chainLayers      []image.ChainLayer
		wantLayerDetails map[extractor.InventoryKey]*extractor.LayerDetails
	}{
		{
			name:             "empty inventory",
			inventory:        []*extractor.Inventory{},
			chainLayers:      []image.ChainLayer{},
			wantLayerDetails: map[extractor.InventoryKey]*extractor.LayerDetails{},
		},
		{
			name: "inventory in single chain layer",
			inventory: []*extractor.Inventory{
				{
					Name:      fooPackage,
					Locations: []string{fooFile},
					Extractor: fakeExtractor1,
				},
				{
					Name:      barPackage,
					Locations: []string{barFile},
					Extractor: fakeExtractor1,
				},
			},
			extractor: fakeExtractor1,
			chainLayers: []image.ChainLayer{
				fakeChainLayer1,
			},
			wantLayerDetails: map[extractor.InventoryKey]*extractor.LayerDetails{
				extractor.InventoryKey{PURL: "pkg:pypi/foo", Path: "foo.txt"}: &extractor.LayerDetails{
					Index:       0,
					DiffID:      "diff-id-1",
					Command:     "command-1",
					InBaseImage: false,
				},
				extractor.InventoryKey{PURL: "pkg:pypi/bar", Path: "bar.txt"}: &extractor.LayerDetails{
					Index:       0,
					DiffID:      "diff-id-1",
					Command:     "command-1",
					InBaseImage: false,
				},
			},
		},
		{
			name: "inventory in two chain layers - package deleted in second layer",
			inventory: []*extractor.Inventory{
				{
					Name:      "foo",
					Locations: []string{fooFile},
					Extractor: fakeExtractor2,
				},
			},
			extractor: fakeExtractor2,
			chainLayers: []image.ChainLayer{
				fakeChainLayer1,
				fakeChainLayer2,
			},
			wantLayerDetails: map[extractor.InventoryKey]*extractor.LayerDetails{
				extractor.InventoryKey{PURL: "pkg:pypi/foo", Path: "foo.txt"}: &extractor.LayerDetails{
					Index:       0,
					DiffID:      "diff-id-1",
					Command:     "command-1",
					InBaseImage: false,
				},
			},
		},
		{
			name: "inventory in multiple chain layers - package added in third layer",
			inventory: []*extractor.Inventory{
				{
					Name:      "foo",
					Locations: []string{fooFile},
					Extractor: fakeExtractor3,
				},
				{
					Name:      "baz",
					Locations: []string{bazFile},
					Extractor: fakeExtractor3,
				},
			},
			extractor: fakeExtractor3,
			chainLayers: []image.ChainLayer{
				fakeChainLayer1,
				fakeChainLayer2,
				fakeChainLayer3,
			},
			wantLayerDetails: map[extractor.InventoryKey]*extractor.LayerDetails{
				extractor.InventoryKey{PURL: "pkg:pypi/foo", Path: "foo.txt"}: &extractor.LayerDetails{
					Index:       0,
					DiffID:      "diff-id-1",
					Command:     "command-1",
					InBaseImage: false,
				},
				extractor.InventoryKey{PURL: "pkg:pypi/baz", Path: "baz.txt"}: &extractor.LayerDetails{
					Index:       2,
					DiffID:      "diff-id-3",
					Command:     "command-3",
					InBaseImage: false,
				},
			},
		},
		{
			name: "inventory in multiple chain layers - bar package added back in last layer",
			inventory: []*extractor.Inventory{
				{
					Name:      "foo",
					Locations: []string{fooFile},
					Extractor: fakeExtractor4,
				},
				{
					Name:      "bar",
					Locations: []string{barFile},
					Extractor: fakeExtractor4,
				},
				{
					Name:      "baz",
					Locations: []string{bazFile},
					Extractor: fakeExtractor4,
				},
			},
			extractor: fakeExtractor4,
			chainLayers: []image.ChainLayer{
				fakeChainLayer1,
				fakeChainLayer2,
				fakeChainLayer3,
				fakeChainLayer4,
			},
			wantLayerDetails: map[extractor.InventoryKey]*extractor.LayerDetails{
				extractor.InventoryKey{PURL: "pkg:pypi/foo", Path: "foo.txt"}: &extractor.LayerDetails{
					Index:       0,
					DiffID:      "diff-id-1",
					Command:     "command-1",
					InBaseImage: false,
				},
				extractor.InventoryKey{PURL: "pkg:pypi/bar", Path: "bar.txt"}: &extractor.LayerDetails{
					Index:       3,
					DiffID:      "diff-id-4",
					Command:     "command-4",
					InBaseImage: false,
				},
				extractor.InventoryKey{PURL: "pkg:pypi/baz", Path: "baz.txt"}: &extractor.LayerDetails{
					Index:       2,
					DiffID:      "diff-id-3",
					Command:     "command-3",
					InBaseImage: false,
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := &filesystem.Config{
				Stats:          stats.NoopCollector{},
				FilesToExtract: []string{"Installed"},
				Extractors:     []filesystem.Extractor{tc.extractor},
			}
			got := ResolveOriginLayer(context.Background(), tc.inventory, tc.chainLayers, config)
			if diff := cmp.Diff(tc.wantLayerDetails, got, cmpopts.IgnoreFields(extractor.Inventory{}, "Extractor")); diff != "" {
				t.Errorf("ResolveOriginLayer(ctx, %v, %v, config) returned an unexpected diff (-want +got): %v", tc.inventory, tc.chainLayers, diff)
			}
		})
	}
}
