/*
Copyright 2026 Intel Corporation

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

package sst

import (
	"fmt"

	"github.com/intel/goresctrl/pkg/utils"
)

// PackageInfo contains SST information for one package.
type PackageInfo struct {
	ID     utils.ID                `json:"id"`
	Punits map[utils.ID]*PunitInfo `json:"punits"`
}

// PunitInfo contains SST information for one power domain (punit) within a package.
type PunitInfo struct {
	CPUs utils.IDSet `json:"cpus"`
	PP   PPInfo      `json:"pp"`
	BF   BFInfo      `json:"bf"`
	TF   TFInfo      `json:"tf"`
	CP   CPInfo      `json:"cp"`
	Clos []ClosInfo  `json:"clos,omitempty"`
}

// PPInfo contains SST-PP (Performance Profile) state.
type PPInfo struct {
	Supported    bool `json:"supported"`
	Locked       bool `json:"locked"`
	Version      int  `json:"version"`
	CurrentLevel int  `json:"currentLevel"`
	MaxLevel     int  `json:"maxLevel"`
}

// BFInfo contains SST-BF (Base Frequency) state.
type BFInfo struct {
	Supported bool        `json:"supported"`
	Enabled   bool        `json:"enabled"`
	Cores     utils.IDSet `json:"cores,omitempty"`
}

// TFInfo contains SST-TF (Turbo Frequency) state.
type TFInfo struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
}

// CPInfo contains SST-CP (Core Power) state.
type CPInfo struct {
	Supported bool           `json:"supported"`
	Enabled   bool           `json:"enabled"`
	Priority  CPPriorityType `json:"priority"`
}

// ClosInfo contains the configuration and CPU associations for one CLOS of SST-CP.
type ClosInfo struct {
	Config ClosConfig  `json:"config"`
	CPUs   utils.IDSet `json:"cpus,omitempty"`
}

// ClosConfig contains the configuration parameters of one CLOS of SST-CP.
type ClosConfig struct {
	ProportionalPriority int `json:"proportionalPriority"`
	MinFreq              int `json:"minFreq"`
	MaxFreq              int `json:"maxFreq"`

	// Legacy fields only supported by the old API and Mbox kernel interface
	epp         int
	desiredFreq int
}

// CPPriorityType denotes the type of CLOS priority ordering used in SST-CP.
type CPPriorityType int

const (
	Proportional CPPriorityType = 0
	Ordered      CPPriorityType = 1
)

// Package provides SST operations for one CPU package.
type Package struct {
	h   *Platform
	pkg *cpuPackageInfo
}

// ID returns the package ID.
func (p *Package) ID() utils.ID {
	return int(p.pkg.id)
}

// GetInfo reads and returns hierarchical SST information for this package.
func (p *Package) GetInfo() (*PackageInfo, error) {
	return p.h.getPackageInfo(p.pkg)
}

// BFEnable enables SST-BF for this package.
// NOTE: The caller should ensure that the sysfs cpufreq scaling limits of the
// affected CPUs allow higher base frequency on high-priority cores; see
// [utils.GetCPUFreqValue], [utils.SetCPUScalingMinFreq], and
// [utils.SetCPUScalingMaxFreq].
func (p *Package) BFEnable() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.bfSetStatus(p.pkg, pu, true); err != nil {
			return err
		}
	}
	return nil
}

// BFDisable disables SST-BF for this package.
func (p *Package) BFDisable() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.bfSetStatus(p.pkg, pu, false); err != nil {
			return err
		}
	}
	return nil
}

// CPEnable enables SST-CP for this package.
func (p *Package) CPEnable() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.cpSetStatus(p.pkg, pu, true); err != nil {
			return err
		}
	}
	return nil
}

// CPDisable disables SST-CP for this package.
func (p *Package) CPDisable() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.cpSetStatus(p.pkg, pu, false); err != nil {
			return err
		}
	}
	return nil
}

// CPReset resets all CLOS parameters to defaults and associates all CPUs to CLOS 0.
func (p *Package) CPReset() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.closResetConfiguration(p.pkg, pu); err != nil {
			return err
		}
	}
	for _, cpu := range p.pkg.cpus.Members() {
		if err := p.h.closAssociate(p.pkg, cpu, 0); err != nil {
			return fmt.Errorf("failed to associate cpu %d to clos 0: %w", cpu, err)
		}
	}
	return nil
}

// CPSetPriorityType sets the SST-CP priority type for this package.
func (p *Package) CPSetPriorityType(priority CPPriorityType) error {
	for _, pu := range p.pkg.punits {
		if err := p.h.cpSetPriorityType(p.pkg, pu, priority); err != nil {
			return err
		}
	}
	return nil
}

// ClosConfigure configures CLOS parameters for this package.
func (p *Package) ClosConfigure(clos int, config ClosConfig) error {
	for _, pu := range p.pkg.punits {
		if err := p.h.closConfigure(p.pkg, pu, clos, &config); err != nil {
			return err
		}
	}
	return nil
}
