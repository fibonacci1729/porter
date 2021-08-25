package runtime

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/hashicorp/go-multierror"

	"get.porter.sh/porter/pkg/config"
	"get.porter.sh/porter/pkg/context"
	"get.porter.sh/porter/pkg/manifest"
	"get.porter.sh/porter/pkg/pkgmgmt"
	"get.porter.sh/porter/pkg/yaml"
	"github.com/cnabio/cnab-go/bundle"
	"github.com/cnabio/cnab-go/bundle/loader"
	"github.com/cnabio/cnab-to-oci/relocation"
	"github.com/pkg/errors"
)

// PorterRuntime orchestrates executing a bundle and managing state.
type PorterRuntime struct {
	*context.Context
	mixins          pkgmgmt.PackageManager
	RuntimeManifest *RuntimeManifest
}

func NewPorterRuntime(cxt *context.Context, mixins pkgmgmt.PackageManager) *PorterRuntime {
	return &PorterRuntime{
		Context: cxt,
		mixins:  mixins,
	}
}

func (r *PorterRuntime) Execute(rm *RuntimeManifest) error {
	r.RuntimeManifest = rm

	installationName := r.Getenv(config.EnvInstallationName)
	bundleName := r.Getenv(config.EnvBundleName)
	fmt.Fprintf(r.Out, "executing %s action from %s (installation: %s)\n", r.RuntimeManifest.Action, bundleName, installationName)

	err := r.RuntimeManifest.Validate()
	if err != nil {
		return err
	}

	// Prepare prepares the runtime environment prior to step execution.
	// As an example, for parameters of type "file", we may need to decode file contents
	// on the filesystem before execution of the step/action
	err = r.RuntimeManifest.Prepare()
	if err != nil {
		return err
	}

	// Update the runtimeManifest images with the bundle.json and relocation mapping (if it's there)
	rtb, reloMap, err := r.getImageMappingFiles()
	if err != nil {
		return err
	}

	err = r.RuntimeManifest.ResolveImages(rtb, reloMap)
	if err != nil {
		return errors.Wrap(err, "unable to resolve bundle images")
	}

	err = r.FileSystem.MkdirAll(context.MixinOutputsDir, 0755)
	if err != nil {
		return errors.Wrapf(err, "could not create outputs directory %s", context.MixinOutputsDir)
	}

	var executionErr error
	for _, step := range r.RuntimeManifest.GetSteps() {
		executionErr = r.executeStep(step)
		if executionErr != nil {
			break
		}
	}

	err = r.applyUnboundBundleOutputs()
	if err != nil {
		// Log but allow the bundle to gracefully exit
		fmt.Fprintln(r.Err, err)
	}

	if executionErr == nil {
		fmt.Fprintln(r.Out, "execution completed successfully!")
	}
	return executionErr // Report the success of the bundle back up the chain
}

func (r *PorterRuntime) executeStep(step *manifest.Step) error {
	if step == nil {
		return nil
	}
	err := r.RuntimeManifest.ResolveStep(step)
	if err != nil {
		return errors.Wrap(err, "unable to resolve step")
	}

	description, _ := step.GetDescription()
	if len(description) > 0 {
		fmt.Fprintln(r.Out, description)
	}

	// Hand over values needing masking in context output streams
	r.Context.SetSensitiveValues(r.RuntimeManifest.GetSensitiveValues())

	input := &ActionInput{
		action: r.RuntimeManifest.Action,
		Steps:  []*manifest.Step{step},
	}
	inputBytes, _ := yaml.Marshal(input)
	cmd := pkgmgmt.CommandOptions{
		Command: string(r.RuntimeManifest.Action),
		Input:   string(inputBytes),
		Runtime: true,
	}
	err = r.mixins.Run(r.Context, step.GetMixinName(), cmd)
	if err != nil {
		return errors.Wrap(err, "mixin execution failed")
	}

	outputs, err := r.readMixinOutputs()
	if err != nil {
		return errors.Wrap(err, "could not read step outputs")
	}

	err = r.RuntimeManifest.ApplyStepOutputs(outputs)
	if err != nil {
		return err
	}

	// Apply any Bundle Outputs declared in this step
	return r.applyStepOutputsToBundle(outputs)
}

func (r *PorterRuntime) createOutputsDir() error {
	// Ensure outputs directory exists
	if err := r.FileSystem.MkdirAll(config.BundleOutputsDir, 0755); err != nil {
		return errors.Wrap(err, "unable to ensure CNAB outputs directory exists")
	}
	return nil
}

// applyStepOutputsToBundle writes the provided step outputs to the proper location
// in the bundle execution environment.
func (r *PorterRuntime) applyStepOutputsToBundle(outputs map[string]string) error {
	err := r.createOutputsDir()
	if err != nil {
		return err
	}

	for outputKey, outputValue := range outputs {
		bundleOutput, ok := r.RuntimeManifest.Outputs[outputKey]
		if !ok {
			continue
		}

		if r.shouldApplyOutput(bundleOutput) {
			outpath := filepath.Join(config.BundleOutputsDir, bundleOutput.Name)

			err := r.FileSystem.WriteFile(outpath, []byte(outputValue), 0755)
			if err != nil {
				return errors.Wrapf(err, "unable to write output file %s", outpath)
			}
		}
	}
	return nil
}

// applyUnboundBundleOutputs find outputs that haven't been bound yet by a step,
// and if they can be bound, i.e. they grab a file from the bundle's filesystem,
// apply the output.
func (r *PorterRuntime) applyUnboundBundleOutputs() error {
	err := r.createOutputsDir()
	if err != nil {
		return err
	}

	if len(r.RuntimeManifest.Outputs) > 0 {
		fmt.Fprintln(r.Out, "Collecting bundle outputs...")
	}

	var bigErr *multierror.Error
	outputs := r.RuntimeManifest.GetOutputs()
	for _, outputDef := range r.RuntimeManifest.Outputs {
		// Ignore outputs that have already been set
		if output := outputs[outputDef.Name]; output != "" {
			continue
		}

		// We can only deal with outputs that are based on a file right now
		if outputDef.Path == "" {
			continue
		}

		if r.shouldApplyOutput(outputDef) {
			outpath := filepath.Join(config.BundleOutputsDir, outputDef.Name)
			err = r.CopyFile(outputDef.Path, outpath)
			if err != nil {
				err = multierror.Append(bigErr, errors.Wrapf(err, "unable to copy output file from %s to %s", outputDef.Path, outpath))
				continue
			}
		}
	}

	return bigErr.ErrorOrNil()
}

func (r *PorterRuntime) shouldApplyOutput(output manifest.OutputDefinition) bool {
	if len(output.ApplyTo) == 0 {
		return true
	}

	for _, applyTo := range output.ApplyTo {
		if string(r.RuntimeManifest.Action) == applyTo {
			return true
		}
	}
	return false
}

func (r *PorterRuntime) readMixinOutputs() (map[string]string, error) {
	outputs := map[string]string{}

	outfiles, err := r.FileSystem.ReadDir(context.MixinOutputsDir)
	if err != nil {
		return nil, errors.Wrapf(err, "could not list %s", context.MixinOutputsDir)
	}

	for _, outfile := range outfiles {
		if outfile.IsDir() {
			continue
		}
		outpath := filepath.Join(context.MixinOutputsDir, outfile.Name())
		contents, err := r.FileSystem.ReadFile(outpath)
		if err != nil {
			return nil, errors.Wrapf(err, "could not read output file %s", outpath)
		}

		outputs[outfile.Name()] = string(contents)

		err = r.FileSystem.Remove(outpath)
		if err != nil {
			return nil, err
		}
	}

	return outputs, nil
}

func (r *PorterRuntime) getImageMappingFiles() (*bundle.Bundle, relocation.ImageRelocationMap, error) {
	l := loader.New()
	bunBytes, err := r.FileSystem.ReadFile("/cnab/bundle.json")
	if err != nil {
		return nil, nil, errors.Wrap(err, "couldn't read runtime bundle.json")
	}
	rtb, err := l.LoadData(bunBytes)
	if err != nil {
		return nil, nil, errors.Wrap(err, "couldn't load runtime bundle.json")
	}
	var reloMap relocation.ImageRelocationMap
	if _, err := r.FileSystem.Stat("/cnab/app/relocation-mapping.json"); err == nil {
		reloBytes, err := r.FileSystem.ReadFile("/cnab/app/relocation-mapping.json")
		if err != nil {
			return nil, nil, errors.Wrap(err, "couldn't read relocation file")
		}
		err = json.Unmarshal(reloBytes, &reloMap)
		if err != nil {
			return nil, nil, errors.Wrap(err, "couldn't load relocation file")
		}
	}
	return rtb, reloMap, nil
}
