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

// Package trace provides functionality to trace the origin of an inventory in a container image.
package trace

import (
	"context"

	scalibrImage "github.com/google/osv-scalibr/artifact/image"
	"github.com/google/osv-scalibr/extractor"
	"github.com/google/osv-scalibr/extractor/filesystem"
	scalibrfs "github.com/google/osv-scalibr/fs"
)

// PopulateLayerDetails populates the LayerDetails field of the inventory with the origin details.
func PopulateLayerDetails(inventory []*extractor.Inventory, originDetails map[extractor.InventoryKey]*extractor.LayerDetails) []*extractor.Inventory {
	inventoryWithLayerDetails := []*extractor.Inventory{}
	for _, inv := range inventory {
		newInv := &extractor.Inventory{
			Name:        inv.Name,
			Version:     inv.Version,
			SourceCode:  inv.SourceCode,
			Locations:   inv.Locations,
			Extractor:   inv.Extractor,
			Annotations: inv.Annotations,
		}

		invKey, err := inv.ToKey()
		if err == nil && originDetails[invKey] != nil {
			newInv.LayerDetails = originDetails[invKey]
		}

		inventoryWithLayerDetails = append(inventoryWithLayerDetails, newInv)
	}
	return inventoryWithLayerDetails
}

// ResolveOriginLayer traces the origin of each inventory in the input.
// It does this by walking the chain layers from newest (last) to oldest (first) and checking if the
// inventory is present in the newer layer. The first layer where the inventory is not present is
// considered to be the layer in which the inventory was introduced.
func ResolveOriginLayer(ctx context.Context, inventory []*extractor.Inventory, chainLayers []scalibrImage.ChainLayer, config *filesystem.Config) map[extractor.InventoryKey]*extractor.LayerDetails {
	layerToCommands := make(map[int]string)
	layerToDiffID := make(map[int]string)
	for i, chainLayer := range chainLayers {
		layerToCommands[i] = chainLayer.Layer().Command()
		layerToDiffID[i] = chainLayer.Layer().DiffID()
	}

	makeExtractorConfig := func(filesToExtract []string, chainFS scalibrfs.FS) *filesystem.Config {
		return &filesystem.Config{
			Stats:                 config.Stats,
			ReadSymlinks:          config.ReadSymlinks,
			Extractors:            config.Extractors,
			DirsToSkip:            config.DirsToSkip,
			SkipDirRegex:          config.SkipDirRegex,
			SkipDirGlob:           config.SkipDirGlob,
			MaxInodes:             config.MaxInodes,
			StoreAbsolutePath:     config.StoreAbsolutePath,
			PrintDurationAnalysis: config.PrintDurationAnalysis,
			// All field values before this are from the Scan Config.
			FilesToExtract: filesToExtract,
			ScanRoots: []*scalibrfs.ScanRoot{
				&scalibrfs.ScanRoot{
					FS: chainFS,
				},
			},
		}
	}

	originDetails := make(map[extractor.InventoryKey]*extractor.LayerDetails)

	for _, inv := range inventory {
		lastChainLayer := chainLayers[len(chainLayers)-1]
		layerIndex := lastChainLayer.Index()

		invKey, err := inv.ToKey()
		if err != nil {
			continue
		}

		originDetails[invKey] = &extractor.LayerDetails{
			Index:       layerIndex,
			DiffID:      layerToDiffID[layerIndex],
			Command:     layerToCommands[layerIndex],
			InBaseImage: false,
		}

		var foundOrigin bool

		// Go backwards through the chain layers and find the first layer where the inventory is not
		// present. Such layer is the layer in which the inventory was introduced. If the inventory is
		// present in all layers, then it means it was introduced in the first layer.
		// TODO: b/381249869 - Optimization: Skip layers if file not found.
		for i := len(chainLayers) - 2; i >= 0; i-- {
			oldChainLayer := chainLayers[i]

			if len(inv.Locations) == 0 {
				// Inventory missing location, cannot trace origin.
				break
			}

			oldInventory, _, err := filesystem.Run(ctx, makeExtractorConfig(inv.Locations, oldChainLayer.FS()))
			if err != nil {
				break
			}

			foundPackage := false
			for _, oldInv := range oldInventory {
				oldInvKey, err := oldInv.ToKey()
				if err != nil {
					continue
				}

				if oldInvKey == invKey {
					foundPackage = true
					break
				}
			}

			// If the inventory is not present in the old layer, then it was introduced in layer i+1.
			if !foundPackage {
				originDetails[invKey] = &extractor.LayerDetails{
					Index:   i + 1,
					DiffID:  layerToDiffID[i+1],
					Command: layerToCommands[i+1],
				}
				foundOrigin = true
				break
			}
		}

		// If the inventory is not present in any layer, then it means it was introduced in the first
		// layer.
		if !foundOrigin {
			originDetails[invKey] = &extractor.LayerDetails{
				Index:   0,
				DiffID:  layerToDiffID[0],
				Command: layerToCommands[0],
			}
		}
	}
	return originDetails
}
