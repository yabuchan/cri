package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	logruscli "github.com/virtual-kubelet/node-cli/logrus"

	constant "github.com/yabuchan/virtual-node-v2/pkg/const"

	"github.com/sirupsen/logrus"
	logruslogger "github.com/virtual-kubelet/virtual-kubelet/log/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/virtual-kubelet/virtual-kubelet/log"

	v1 "k8s.io/api/core/v1"

	"github.com/yabuchan/virtual-node-v2/pkg/persistence"
)

func main() {
	logger := logrus.StandardLogger()
	log.L = logruslogger.FromLogrus(logrus.NewEntry(logger))
	logConfig := &logruscli.Config{LogLevel: "debug"}
	logruscli.Configure(logConfig, logger)

	testPodName1 := "test-pod1"
	testPodName2 := "test-pod2"

	ctx := context.Background()
	ph, err := persistence.NewProviderHelper(constant.QueueBaseName, constant.TableName, constant.DeviceId)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	go ph.StartReceivingMessage(ctx)

	err = ph.SetupQueue(ctx)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	pod1 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: testPodName1,
		},
		Spec: v1.PodSpec{
			NodeName: "test",
		},
		Status: v1.PodStatus{
			Phase: v1.PodPending,
		},
	}
	pod2 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: testPodName2,
		},
		Spec: v1.PodSpec{
			NodeName: "test",
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
		},
	}
	err = ph.SendPods(ctx, []*v1.Pod{pod1, pod2}, persistence.RequestTypeGetPods)
	if err != nil {
		log.G(ctx).WithField("err", err).Fatal("failed to send posts")
		return
	}
	for _, pod := range []*v1.Pod{pod1, pod2} {
		err = ph.CreatePod(ctx, pod)
		if err != nil {
			log.G(ctx).WithField("err", err).Fatal("failed to create pod")
			return
		}
	}

	time.Sleep(10 * time.Second)
	pods, err := ph.GetPods(ctx)
	if err != nil {
		log.G(ctx).WithField("err", err).Fatal("failed to get pods")
		return
	}
	log.G(ctx).Infof("# of pods: %d", len(pods))

	p, err := ph.GetPod(ctx, "", testPodName1)
	if err != nil {
		log.G(ctx).Error("failed to get pod. %s", err.Error())
		return
	}
	fmt.Printf("pod: %+v\n", p)

	podStatus, err := ph.GetPodStatus(ctx, "", testPodName2)
	if err != nil {
		log.G(ctx).Error("failed to get pod status. %s", err.Error())
		return
	}
	fmt.Printf("pod status: %+v\n", podStatus)

	// Watch OS signal
	osSignal := make(chan os.Signal, 1)
	go signal.Notify(osSignal, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		log.G(ctx).Info("Server is shutting down")
		break
	case s := <-osSignal:
		log.G(ctx).WithField("reason", s.String()).Info("Server is shutting down due to key interruption")
		ctx.Done()
		break
	}
}
