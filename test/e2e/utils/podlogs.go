/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package utils

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/pkg/errors"
	"io"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// LogsForPod starts reading the logs for a certain pod. If the pod has more than one
// container, opts.Container must be set. Reading stops when the context is done.
func LogsForPod(ctx context.Context, cs clientset.Interface, ns, pod string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	req := cs.Core().Pods(ns).GetLogs(pod, opts)
	return req.Context(ctx).Stream()
}

// CopyAllLogs follows the logs of all containers in the pod and writes each log line
// with the container name as prefix. It does that until the context is done or
// until an error occurs.
func CopyAllLogs(ctx context.Context, cs clientset.Interface, ns, pod string, to io.Writer) error {
	p, err := cs.Core().Pods(ns).Get(pod, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "get pod %s in %s", pod, ns)
	}
	watcher, err := cs.Core().Pods(ns).Watch(metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "cannot create Pod event watcher")
	}

	go func() {
		var m sync.Mutex
		logging := map[string]bool{}
		check := func() {
			m.Lock()
			defer m.Unlock()
			for _, c := range p.Spec.Containers {
				if logging[c.Name] {
					continue
				}
				readCloser, err := LogsForPod(ctx, cs, ns, pod,
					&corev1.PodLogOptions{
						Container: c.Name,
						Follow:    true,
					})
				if err != nil {
					fmt.Fprintf(to, "%s: %s\n", c.Name, err)
					continue
				}
				go func(container string) {
					defer func() {
						m.Lock()
						logging[container] = false
						m.Unlock()
						readCloser.Close()
					}()
					scanner := bufio.NewScanner(readCloser)
					for scanner.Scan() {
						fmt.Fprintf(to, "%s: %s\n", container, scanner.Text())
					}
				}(c.Name)
				logging[c.Name] = true
			}
		}

		// Watch events to see whether we can start logging
		// and log interesting ones.
		check()
		for {
			select {
			case <-watcher.ResultChan():
				check()
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// WatchPods prints pod status events.
func WatchPods(ctx context.Context, cs clientset.Interface, to io.Writer) error {
	watcher, err := cs.Core().Pods("").Watch(metav1.ListOptions{})
	if err != nil {
		return errors.Wrap(err, "cannot create Pod event watcher")
	}

	go func() {
		defer watcher.Stop()
		for {
			select {
			case e := <-watcher.ResultChan():
				if e.Object == nil {
					continue
				}

				pod, ok := e.Object.(*corev1.Pod)
				if !ok {
					continue
				}
				buffer := new(bytes.Buffer)
				fmt.Fprintf(buffer,
					"pod event: %s: %s/%s %s: %s %s\n",
					e.Type,
					pod.Namespace,
					pod.Name,
					pod.Status.Phase,
					pod.Status.Reason,
					pod.Status.Conditions,
				)
				for _, cst := range pod.Status.ContainerStatuses {
					fmt.Fprintf(buffer, "   %s: ", cst.Name)
					if cst.State.Waiting != nil {
						fmt.Fprintf(buffer, "WAITING: %s - %s",
							cst.State.Waiting.Reason,
							cst.State.Waiting.Message,
						)
					} else if cst.State.Running != nil {
						fmt.Fprintf(buffer, "RUNNING")
					} else if cst.State.Waiting != nil {
						fmt.Fprintf(buffer, "TERMINATED: %s - %s",
							cst.State.Waiting.Reason,
							cst.State.Waiting.Message,
						)
					}
					fmt.Fprintf(buffer, "\n")
				}
				to.Write(buffer.Bytes())
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}
