package lxcontainer

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type Namespace struct {
	Name      string
	CloneFlag int
}

var CgroupNamespace = Namespace{"cgroup", unix.CLONE_NEWCGROUP}
var IPCNamespace = Namespace{"ipc", unix.CLONE_NEWIPC}
var MountNamespace = Namespace{"mnt", unix.CLONE_NEWNS}
var NetworkNamespace = Namespace{"net", unix.CLONE_NEWNET}
var PIDNamespace = Namespace{"pid", unix.CLONE_NEWPID}
var TimeNamespace = Namespace{"time", unix.CLONE_NEWTIME}
var UserNamespace = Namespace{"user", unix.CLONE_NEWUSER}
var UTSNamespace = Namespace{"uts", unix.CLONE_NEWUTS}

// maps from CRIO namespace names to LXC names and clone flags
var namespaceMap = map[specs.LinuxNamespaceType]Namespace{
	specs.CgroupNamespace:  CgroupNamespace,
	specs.IPCNamespace:     IPCNamespace,
	specs.MountNamespace:   MountNamespace,
	specs.NetworkNamespace: NetworkNamespace,
	specs.PIDNamespace:     PIDNamespace,
	// specs.TimeNamespace:     TimeNamespace,
	specs.UserNamespace: UserNamespace,
	specs.UTSNamespace:  UTSNamespace,
}

func cloneFlags(namespaces []specs.LinuxNamespace) (int, error) {
	flags := 0
	for _, ns := range namespaces {
		n, exist := namespaceMap[ns.Type]
		if !exist {
			return 0, fmt.Errorf("namespace %s is not supported", ns.Type)
		}
		flags |= n.CloneFlag
	}
	return flags, nil
}

func configureNamespaces(c *Container) error {
	seenNamespaceTypes := map[specs.LinuxNamespaceType]bool{}
	cloneNamespaces := make([]string, 0, len(c.Linux.Namespaces))

	for _, ns := range c.Linux.Namespaces {
		if _, seen := seenNamespaceTypes[ns.Type]; seen {
			return fmt.Errorf("duplicate namespace %s", ns.Type)
		}
		seenNamespaceTypes[ns.Type] = true

		n, supported := namespaceMap[ns.Type]
		if !supported {
			return fmt.Errorf("unsupported namespace %s", ns.Type)
		}

		if ns.Path == "" {
			cloneNamespaces = append(cloneNamespaces, n.Name)
			continue
		}

		configKey := fmt.Sprintf("lxc.namespace.share.%s", n.Name)
		if err := c.SetConfigItem(configKey, ns.Path); err != nil {
			return err
		}
	}

	return c.SetConfigItem("lxc.namespace.clone", strings.Join(cloneNamespaces, " "))
}

func isNamespaceEnabled(spec *specs.Spec, nsType specs.LinuxNamespaceType) bool {
	for _, ns := range spec.Linux.Namespaces {
		if ns.Type == nsType {
			return true
		}
	}
	return false
}

func getNamespace(nsType specs.LinuxNamespaceType, namespaces []specs.LinuxNamespace) *specs.LinuxNamespace {
	for _, n := range namespaces {
		if n.Type == nsType {
			return &n
		}
	}
	return nil
}

// lxc does not set the hostname on shared namespaces
func setHostname(nsPath string, hostname string) error {
	// setns only affects the current thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	f, err := os.Open(nsPath)
	if err != nil {
		return fmt.Errorf("failed to open container uts namespace %q: %w", nsPath, err)
	}
	// #nosec
	defer f.Close()

	self, err := os.Open("/proc/self/ns/uts")
	if err != nil {
		return fmt.Errorf("failed to open uts namespace : %w", err)
	}
	// #nosec
	defer func() {
		unix.Setns(int(self.Fd()), unix.CLONE_NEWUTS)
		self.Close()
	}()

	err = unix.Setns(int(f.Fd()), unix.CLONE_NEWUTS)
	if err != nil {
		return fmt.Errorf("failed to switch to UTS namespace %s: %w", nsPath, err)
	}
	err = unix.Sethostname([]byte(hostname))
	if err != nil {
		return fmt.Errorf("unix.Sethostname failed: %w", err)
	}
	return nil
}
