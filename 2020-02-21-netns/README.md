# Create and enter Linux network namespaces

This sketch shows how to create named Linux network namespaces and join
them using go's [`syscall`](https://pkg.go.dev/syscall?tab=doc) package.
Setup follows the same conventions as the standard
[iproute2](https://wiki.linuxfoundation.org/networking/iproute2)
tooling in `ip netns`, so they should be interoperable.

(At least as interoperable as we can offer in a one-evening sketch, that is.)

## Build

```
make
```

## Example

Run `ip link` within the `ns0` namespace, creating it if need be:

```
sudo build/setns ns0 ip link
```

Verify a different namespace set up by `iproute2`, `ns1`, interoperates
with our binary:

```
sudo ip link add veth0 type veth peer name veth1
sudo ip netns add ns1
sudo ip link set veth1 netns ns1
sudo build/setns ns1 ip link
```

## How it works

The blog's got a longer explanation, but here's the absolute minimum you
need to know:

1. A process can have only one network namespace at a time, but it can
   switch to a new or existing namespace using 
   [clone(2)](https://manpages.debian.org/buster/manpages-dev/clone.2.en.html),
   [unshare(2)](https://manpages.debian.org/buster/manpages-dev/unshare.2.en.html),
   and 
   [setns(2)](https://manpages.debian.org/buster/manpages-dev/setns.2.en.html) syscalls.
1. To create a network namespace, you can either (a) 
   [clone(2)](https://manpages.debian.org/buster/manpages-dev/clone.2.en.html)
   your process into a new process in a new namespace or (b)
   [unshare(2)](https://manpages.debian.org/buster/manpages-dev/unshare.2.en.html)
   your existing process into a new namespace.
   (We're only interested in creating namespaces from within our process, so it's unshare for us.)
1. To refer to a specific namespace (e.g. to switch the namespace of
   your process with [setns(2)](https://manpages.debian.org/buster/manpages-dev/setns.2.en.html)),
   you must use a file descriptor, pointing to some file you've opened
   in `/proc`.
1. A process's current namespace can be found in `/proc/self/ns/net`
   from within the process, or from `/proc/${PID}/ns/net` anywhere.
1. "Named" namespaces are just a convention from `iproute2`, where `ip
   netns add ${NAME}` creates a new namespace and then bind mounts the
   current `/proc/self/ns/net` to a new, empty file in
   `/var/run/netns/${NAME}`. Effectively, this lets you keep a network
   namespace going even when no process is running in it (or has an open
   file descriptor point to it.)

Aside: tonight I learned you can bind mount files, but that when you do,
you must create the target file first.

## Other things to keep in mind

You'll notice that `execNamespace()` calls `runtime.LockOSThread()` and
defers unlocking the OS thread. This is critical, because:

1. A go program is almost always a collection of goroutines being run by
   the scheduler across not one process, but multiple threads.
1. The syscalls we do in this sketch are all expected to happen in a single
   process.
1. Without the lock, we thus have no guarantee that all the syscall and
   mount code is going to happen safely (in the same process).
