// Minimal tool for creating & executing commands within a named Linux
// network namespace, following iproute2's conventions for them.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

// SYS_SETNS syscall allows changing the namespace of the current process.
var SYS_SETNS = map[string]uintptr{
	"386":      346,
	"amd64":    308,
	"arm64":    268,
	"arm":      375,
	"mips":     4344,
	"mipsle":   4344,
	"mips64le": 4344,
	"ppc64":    350,
	"ppc64le":  350,
	"riscv64":  268,
	"s390x":    339,
}[runtime.GOARCH]

// Opens the file for a given namespace
func openNamespace(name string) (*os.File, error) {
	log.Printf("openNamespace: %s", name)
	return os.Open(filepath.Join("/run/netns", name))
}

// Log any new error. For use when closing a file during error handling.
func closeFile(f *os.File) {
	if err := f.Close(); err != nil {
		log.Printf("close file error: %v", err)
	}
}

// Unshare into a new namespace, returning the original namespace.
func unshare() (*os.File, error) {
	log.Printf("unshare")
	f, err := os.Open("/proc/self/ns/net")
	if err != nil {
		return nil, err
	}

	// Cf. strace ip netns add some-namespace
	_, _, e1 := syscall.Syscall(syscall.SYS_UNSHARE, syscall.CLONE_NEWNET, 0, 0)
	if e1 != 0 {
		closeFile(f)
		return nil, e1
	}
	return f, nil
}

// Sets namespace to the given open file.
func setns(f *os.File) error {
	log.Printf("setns: name=%s fd=%d", f.Name(), f.Fd())
	// Cf. https://github.com/vishvananda/netns/blob/master/netns_linux.go#L41
	_, _, e1 := syscall.Syscall(SYS_SETNS, f.Fd(), syscall.CLONE_NEWNET, 0)
	if e1 != 0 {
		return e1
	}
	return nil
}

const nsDir = "/var/run/netns"

// Mount /var/run/netns as tmpfs if it doesn't already exist
func mountNamespaceDir() error {
	log.Printf("mountNamespaceDir")
	// strace fragment:
	//
	// 	mkdir("/var/run/netns", 0755)           = 0
	// 	mount("", "/var/run/netns", 0x56177d0e29a5, MS_REC|MS_SHARED, NULL) = -1 EINVAL (Invalid argument)
	// 	mount("/var/run/netns", "/var/run/netns", 0x56177d0e29a5, MS_BIND|MS_REC, NULL) = 0
	// 	mount("", "/var/run/netns", 0x56177d0e29a5, MS_REC|MS_SHARED, NULL) = 0
	_, err := os.Stat(nsDir)
	switch {
	case err == nil:
		// NB: looking at fragment above, iproute2 does one better
		// and checks that the dir is actually mounted tmpfs.
		return nil

	case os.IsNotExist(err):
		err = os.MkdirAll(nsDir, 0755)
		if err != nil {
			return err
		}
		return syscall.Mount(
			nsDir, nsDir, "tmpfs", syscall.MS_BIND|syscall.MS_REC, "")

	default:
		return err
	}
}

// Enters a new namespace and bind mounts it to
// /var/run/netns/${name}, returning an open file to the
// original namespace.
func createNamespace(name string) (*os.File, error) {
	// strace fragment:
	//
	// 	openat(AT_FDCWD, "/var/run/netns/ns0", O_RDONLY|O_CREAT|O_EXCL, 000) = 5
	// 	close(5)                                = 0
	// 	unshare(CLONE_NEWNET)                   = 0
	// 	mount("/proc/self/ns/net", "/var/run/netns/ns0", 0x56177d0e29a5, MS_BIND, NULL) = 0
	log.Printf("createNamespace")
	f, err := unshare()
	if err != nil {
		return nil, err
	}

	if err := mountNamespaceDir(); err != nil {
		closeFile(f)
		return nil, err
	}

	nsPath := filepath.Join(nsDir, name)
	f, err = os.Create(nsPath)
	if err != nil {
		return nil, err
	}
	if f.Close(); err != nil {
		return nil, err
	}

	err = syscall.Mount("/proc/self/ns/net", nsPath, "", syscall.MS_BIND, "")
	if err != nil {
		closeFile(f)
		return nil, err
	}
	return f, nil
}

// Sets namespace to /var/run/netns/${name}, creating
// that namespace if necessary.
//
// Returns an open file pointing to the original namespace.
func setNamespace(name string) (*os.File, error) {
	log.Printf("setNamespace: %s", name)
	newFile, err := openNamespace(name)
	if os.IsNotExist(err) {
		origFile, err := createNamespace(name)
		if err != nil {
			return nil, err
		}
		return origFile, nil
	} else if err != nil {
		return nil, err
	}

	origFile, err := os.Open("/proc/self/ns/net")
	if err != nil {
		closeFile(newFile)
		return nil, err
	}

	if err := setns(newFile); err != nil {
		closeFile(newFile)
		closeFile(origFile)
		return nil, err
	}

	return origFile, nil
}

// Executes a command in a given namespace.
func execNamespace(name string, args []string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	f, err := setNamespace(name)
	if err != nil {
		return err
	}

	arg0, err := exec.LookPath(args[0])
	if err != nil {
		closeFile(f)
		return err
	}
	return syscall.Exec(arg0, args, os.Environ())
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(
			flag.CommandLine.Output(),
			"Usage: %s NAMESPACE COMMAND ARG...\n",
			os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	if len(args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	if err := execNamespace(args[0], args[1:]); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}
