// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/config/data"
	"github.com/derailed/k9s/internal/config/json"
	"github.com/derailed/k9s/internal/slogs"
	"github.com/derailed/k9s/internal/view/cmd"
	"gopkg.in/yaml.v3"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// Config tracks K9s configuration options.
type Config struct {
	K9s      *K9s `yaml:"k9s" json:"k9s"`
	conn     client.Connection
	settings data.KubeSettings
}

// NewConfig creates a new default config.
func NewConfig(ks data.KubeSettings) *Config {
	return &Config{
		settings: ks,
		K9s:      NewK9s(nil, ks),
	}
}

// IsReadOnly returns true if K9s is running in read-only mode.
func (c *Config) IsReadOnly() bool {
	return c.K9s.IsReadOnly()
}

// ActiveClusterName returns the corresponding cluster name.
func (c *Config) ActiveClusterName(contextName string) (string, error) {
	ct, err := c.settings.GetContext(contextName)
	if err != nil {
		return "", err
	}

	return ct.Cluster, nil
}

// ContextHotkeysPath returns a context specific hotkeys file spec.
func (c *Config) ContextHotkeysPath() string {
	ct, err := c.K9s.ActiveContext()
	if err != nil {
		return ""
	}

	return AppContextHotkeysFile(ct.ClusterName, c.K9s.activeContextName)
}

// ContextAliasesPath returns a context specific aliases file spec.
func (c *Config) ContextAliasesPath() string {
	ct, err := c.K9s.ActiveContext()
	if err != nil {
		return ""
	}

	return AppContextAliasesFile(ct.GetClusterName(), c.K9s.activeContextName)
}

// ContextPluginsPath returns a context specific plugins file spec.
func (c *Config) ContextPluginsPath() (string, error) {
	ct, err := c.K9s.ActiveContext()
	if err != nil {
		return "", err
	}

	return AppContextPluginsFile(ct.GetClusterName(), c.K9s.activeContextName), nil
}

func setK8sTimeout(flags *genericclioptions.ConfigFlags, d time.Duration) {
	v := d.String()
	flags.Timeout = &v
}

// Refine the configuration based on cli args.
func (c *Config) Refine(flags *genericclioptions.ConfigFlags, k9sFlags *Flags, cfg *client.Config) error {
	if flags == nil {
		return nil
	}

	if !isStringSet(flags.Timeout) {
		if d, err := time.ParseDuration(c.K9s.APIServerTimeout); err == nil {
			setK8sTimeout(flags, d)
		} else {
			setK8sTimeout(flags, client.DefaultCallTimeoutDuration)
		}
	}
	if isStringSet(flags.Context) {
		if _, err := c.K9s.ActivateContext(*flags.Context); err != nil {
			return fmt.Errorf("k8sflags. unable to activate context %q: %w", *flags.Context, err)
		}
	} else {
		n, err := cfg.CurrentContextName()
		if err != nil {
			return fmt.Errorf("unable to retrieve kubeconfig current context %q: %w", n, err)
		}
		_, err = c.K9s.ActivateContext(n)
		if err != nil {
			return fmt.Errorf("unable to activate context %q: %w", n, err)
		}
	}
	slog.Debug("Using active context", slogs.Context, c.K9s.ActiveContextName())

	var ns string
	switch {
	case k9sFlags != nil && IsBoolSet(k9sFlags.AllNamespaces):
		ns = client.NamespaceAll
		c.ResetActiveView()
	case isStringSet(flags.Namespace):
		ns = *flags.Namespace
		c.ResetActiveView()
	default:
		nss, err := c.K9s.ActiveContextNamespace()
		if err != nil {
			return err
		}
		ns = nss
	}
	if ns == "" {
		ns = client.DefaultNamespace
	}
	if err := c.SetActiveNamespace(ns); err != nil {
		return err
	}

	return data.EnsureDirPath(c.K9s.AppScreenDumpDir(), data.DefaultDirMod)
}

// Reset resets the context to the new current context/cluster.
func (c *Config) Reset() {
	c.K9s.Reset()
}

func (c *Config) ActivateContext(n string) (*data.Context, error) {
	ct, err := c.K9s.ActivateContext(n)
	if err != nil {
		return nil, fmt.Errorf("set current context failed. %w", err)
	}

	return ct, nil
}

// CurrentContext fetch the configuration active context.
func (c *Config) CurrentContext() (*data.Context, error) {
	return c.K9s.ActiveContext()
}

// ActiveNamespace returns the active namespace in the current context.
// If none found return the empty ns.
func (c *Config) ActiveNamespace() string {
	ns, err := c.K9s.ActiveContextNamespace()
	if err != nil {
		slog.Error("Unable to assert active namespace. Using default", slogs.Error, err)
		ns = client.DefaultNamespace
	}

	return ns
}

// FavNamespaces returns fav namespaces in the current context.
func (c *Config) FavNamespaces() []string {
	ct, err := c.K9s.ActiveContext()
	if err != nil {
		return nil
	}
	ct.Validate(c.conn, c.K9s.getActiveContextName(), ct.ClusterName)

	return ct.Namespace.Favorites
}

// SetActiveNamespace set the active namespace in the current context.
func (c *Config) SetActiveNamespace(ns string) error {
	if ns == client.NotNamespaced {
		slog.Debug("No namespace given. skipping!", slogs.Namespace, ns)
		return nil
	}
	ct, err := c.K9s.ActiveContext()
	if err != nil {
		return err
	}

	return ct.Namespace.SetActive(ns, c.settings)
}

// ActiveView returns the active view in the current context.
func (c *Config) ActiveView() string {
	ct, err := c.K9s.ActiveContext()
	if err != nil {
		return data.DefaultView
	}
	v := ct.View.Active
	if c.K9s.manualCommand != nil && *c.K9s.manualCommand != "" {
		v = *c.K9s.manualCommand
		// We reset the manualCommand property because
		// the command-line switch should only be considered once,
		// on startup.
		*c.K9s.manualCommand = ""
	}

	return v
}

func (c *Config) ResetActiveView() {
	if isStringSet(c.K9s.manualCommand) {
		return
	}
	v := c.ActiveView()
	if v == "" {
		return
	}
	p := cmd.NewInterpreter(v)
	if p.HasNS() {
		c.SetActiveView(p.Cmd())
	}
}

// SetActiveView sets current context active view.
func (c *Config) SetActiveView(view string) {
	if ct, err := c.K9s.ActiveContext(); err == nil {
		ct.View.Active = view
	}
}

// GetConnection return an api server connection.
func (c *Config) GetConnection() client.Connection {
	return c.conn
}

// SetConnection set an api server connection.
func (c *Config) SetConnection(conn client.Connection) {
	c.conn = conn
	if conn != nil {
		c.K9s.resetConnection(conn)
	}
}

func (c *Config) ActiveContextName() string {
	return c.K9s.activeContextName
}

func (c *Config) Merge(c1 *Config) {
	c.K9s.Merge(c1.K9s)
}

// Load loads K9s configuration from file.
func (c *Config) Load(path string, force bool) error {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := c.Save(force); err != nil {
			return err
		}
	}
	bb, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var errs error
	if err := data.JSONValidator.Validate(json.K9sSchema, bb); err != nil {
		errs = errors.Join(errs, fmt.Errorf("k9s config file %q load failed:\n%w", path, err))
	}

	var cfg Config
	if err := yaml.Unmarshal(bb, &cfg); err != nil {
		errs = errors.Join(errs, fmt.Errorf("main config.yaml load failed: %w", err))
	}
	c.Merge(&cfg)

	return errs
}

// Save configuration to disk.
func (c *Config) Save(force bool) error {
	contextName := c.K9s.ActiveContextName()
	clusterName, err := c.ActiveClusterName(contextName)
	if err != nil {
		return fmt.Errorf("unable to locate associated cluster for context %q: %w", contextName, err)
	}
	c.Validate(contextName, clusterName)
	if err := c.K9s.Save(contextName, clusterName, force); err != nil {
		return err
	}
	if _, err := os.Stat(AppConfigFile); errors.Is(err, fs.ErrNotExist) {
		return c.SaveFile(AppConfigFile)
	}

	return nil
}

// SaveFile K9s configuration to disk.
func (c *Config) SaveFile(path string) error {
	if err := data.EnsureDirPath(path, data.DefaultDirMod); err != nil {
		return err
	}

	if err := data.SaveYAML(path, c); err != nil {
		slog.Error("Unable to save K9s config file", slogs.Error, err)
		return err
	}

	slog.Info("[CONFIG] Saving K9s config to disk", slogs.Path, path)
	return nil
}

// Validate the configuration.
func (c *Config) Validate(contextName, clusterName string) {
	if c.K9s == nil {
		c.K9s = NewK9s(c.conn, c.settings)
	}
	c.K9s.Validate(c.conn, contextName, clusterName)
}

// Dump for debug...
func (c *Config) Dump(msg string) {
	ct, err := c.K9s.ActiveContext()
	if err == nil {
		bb, _ := yaml.Marshal(ct)
		fmt.Printf("Dump: %q\n%s\n", msg, string(bb))
	} else {
		fmt.Println("BOOM!", err)
	}
}
