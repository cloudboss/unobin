package replacement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
)

type Configuration struct {
	Directory string
}

type Object struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type Output struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func Library() *runtime.Library {
	name := runtime.InputField(func(v *Object) *string { return &v.Name })
	size := runtime.InputField(func(v *Object) *int64 { return &v.Size })
	return &runtime.Library{
		Name: "replacement",
		Configuration: &cfg.ConfigurationType[*Configuration]{
			SchemaVersion: 1,
			New:           func() *Configuration { return &Configuration{} },
		},
		Resources: map[string]runtime.ResourceRegistration{
			"object": runtime.MakeResource[Object, *Output, *Configuration](
				runtime.ResourceDefinition[Object, *Output, *Configuration]{
					SchemaVersion: 1,
					Identity: runtime.ResourceIdentity[Object, *Output]{
						Version: 1,
						Scope:   runtime.IdentityConfiguration,
						StableID: func(_ Object, output *Output) (string, error) {
							return output.ID, nil
						},
					},
					InputSemantics: runtime.InputSemantics[Object]{
						Rules: []runtime.InputRule[Object]{
							runtime.EqualBy(name, func(a, b string) bool {
								a = strings.TrimPrefix(a, "ref:")
								b = strings.TrimPrefix(b, "ref:")
								return a == b
							}),
						},
					},
					Replacement: runtime.ReplacementRules[Object, *Output]{
						Inputs: []runtime.ReplacementRule[Object]{
							runtime.ReplaceWhenChanged(name),
							runtime.ReplaceWhen(size, func(prior, desired int64) bool {
								return desired < prior
							}),
						},
						Drift: []runtime.DriftRule[*Output]{
							runtime.ReplaceOnDrift(
								runtime.OutputField(func(v *Output) *int64 { return &v.Size }),
								func(a, b int64) bool { return a == b },
							),
						},
					},
				},
			),
		},
		Actions: map[string]runtime.ActionRegistration{
			"record": runtime.MakeAction[Record, *Recorded, *Configuration](),
		},
	}
}

func (o *Object) Create(_ context.Context, config *Configuration) (*Output, error) {
	if err := o.record(config, "create", nil); err != nil {
		return nil, err
	}
	if os.Getenv("E2E_FAIL_CREATE") == "1" {
		return nil, errors.New("object create failed")
	}
	path := filepath.Join(config.Directory, "generation")
	body, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var generation int
	if len(body) > 0 {
		generation, err = strconv.Atoi(string(body))
		if err != nil {
			return nil, err
		}
	}
	generation++
	if err := os.WriteFile(path, []byte(strconv.Itoa(generation)), 0o600); err != nil {
		return nil, err
	}
	id := fmt.Sprintf("object-%d", generation)
	if os.Getenv("E2E_CREATE_EMPTY_ID") == "1" {
		id = ""
	}
	return o.write(config, id)
}

func (o *Object) Read(_ context.Context, config *Configuration, _ *Output) (*Output, error) {
	body, err := os.ReadFile(filepath.Join(config.Directory, "object.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, runtime.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var output Output
	if err := json.Unmarshal(body, &output); err != nil {
		return nil, err
	}
	if size := os.Getenv("E2E_REMOTE_SIZE"); size != "" {
		output.Size, err = strconv.ParseInt(size, 10, 64)
		if err != nil {
			return nil, err
		}
	}
	if name := os.Getenv("E2E_REMOTE_NAME"); name != "" {
		output.Name = name
	}
	if id := os.Getenv("E2E_REMOTE_ID"); id != "" {
		output.ID = id
	}
	if err := writeOutput(config, &output); err != nil {
		return nil, err
	}
	if err := o.record(config, "read", &output); err != nil {
		return nil, err
	}
	return &output, nil
}

func (o *Object) Update(
	_ context.Context, config *Configuration, prior runtime.Prior[Object, *Output],
) (*Output, error) {
	if err := o.record(config, "update", prior.Observed); err != nil {
		return nil, err
	}
	id := prior.Observed.ID
	if changed := os.Getenv("E2E_UPDATE_ID"); changed != "" {
		id = changed
	}
	return o.write(config, id)
}

func (o *Object) Delete(_ context.Context, config *Configuration, prior *Output) error {
	if err := o.record(config, "delete", prior); err != nil {
		return err
	}
	if os.Getenv("E2E_FAIL_DELETE") == "1" {
		return errors.New("object delete failed")
	}
	return os.Remove(filepath.Join(config.Directory, "object.json"))
}

func (o *Object) write(config *Configuration, id string) (*Output, error) {
	output := &Output{ID: id, Name: strings.TrimPrefix(o.Name, "ref:"), Size: o.Size}
	if err := writeOutput(config, output); err != nil {
		return nil, err
	}
	return output, nil
}

func writeOutput(config *Configuration, output *Output) error {
	body, err := json.Marshal(output)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(config.Directory, "object.json"), body, 0o600)
}

func (o *Object) record(config *Configuration, method string, output *Output) error {
	if err := os.MkdirAll(config.Directory, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(config.Directory, "events.ndjson"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(file).Encode(struct {
		Method string  `json:"method"`
		Inputs *Object `json:"inputs"`
		Prior  *Output `json:"prior,omitempty"`
	}{Method: method, Inputs: o, Prior: output})
	return errors.Join(err, file.Close())
}

type Record struct {
	ID string
}

type Recorded struct {
	ID string
}

func (r *Record) Run(_ context.Context, config *Configuration) (*Recorded, error) {
	file, err := os.OpenFile(filepath.Join(config.Directory, "recorded.ndjson"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	output := &Recorded{ID: r.ID}
	err = json.NewEncoder(file).Encode(output)
	if err := errors.Join(err, file.Close()); err != nil {
		return nil, err
	}
	return output, nil
}
