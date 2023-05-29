package persistence

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/virtual-kubelet/virtual-kubelet/log"

	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/json"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	v1 "k8s.io/api/core/v1"
)

type ProviderHelper interface {
	CreatePod(ctx context.Context, pod *v1.Pod) error
	DeletePod(ctx context.Context, pod *v1.Pod) error
	GetPod(ctx context.Context, namespace, name string) (*v1.Pod, error)
	GetPodStatus(ctx context.Context, namespace, name string) (*v1.PodStatus, error)
	GetPods(ctx context.Context) ([]*v1.Pod, error)
	SetupQueue(ctx context.Context) error
	StartReceivingMessage(ctx context.Context)

	// SendPods is for testing
	SendPods(ctx context.Context, pods []*v1.Pod, requestType RequestType) error
}

type queue struct {
	queueBaseName     *string
	sendQueueUrl      *string
	receiveQueueUrl   *string
	deadQueueUrl      *string
	messageAttribute  map[string]*sqs.MessageAttributeValue
	groupId           *string
	client            *sqs.SQS
	visibilityTimeout *int64
	waitTimeSeconds   *int64
	region            *string
	maxRetry          *int64
}

type db struct {
	tableName string
	client    *dynamodb.DynamoDB
}

type providerHelper struct {
	nodeId *string
	queue  *queue
	db     *db
}

type PodModel struct {
	PodName string
	Pod     *v1.Pod
	NodeId  string
}

const (
	DYNAMODB_INDEX_NAME_NODEID  = "NodeId"
	DYNAMODB_INDEX_NAME_PODNAME = "PodName"
)

func NewProviderHelper(queueBaseName string, tableName string, deviceId string) (ProviderHelper, error) {
	region := aws.String("ap-northeast-1")
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config: aws.Config{
			Region: region,
		},
	}))

	sqsClient := sqs.New(sess)
	dynamodbClient := dynamodb.New(sess)

	attr := map[string]*sqs.MessageAttributeValue{
		"Title": &sqs.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String("The Whistler"),
		},
		"Author": &sqs.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String("John Grisham"),
		},
		"WeeksOn": &sqs.MessageAttributeValue{
			DataType:    aws.String("Number"),
			StringValue: aws.String("6"),
		},
	}

	return &providerHelper{
		nodeId: aws.String(deviceId),
		queue: &queue{
			queueBaseName:     aws.String(queueBaseName),
			messageAttribute:  attr,
			client:            sqsClient,
			visibilityTimeout: aws.Int64(10),
			waitTimeSeconds:   aws.Int64(20),
			region:            region,
			maxRetry:          aws.Int64(3),
		},
		db: &db{
			tableName: tableName,
			client:    dynamodbClient,
		},
	}, nil
}

func (p *providerHelper) SetupQueue(ctx context.Context) (err error) {
	sendQueueName := aws.String(*p.queue.queueBaseName + "-to_edge")
	receiveQueueName := aws.String(*p.queue.queueBaseName + "-to_core")
	deadQueueName := aws.String(*p.queue.queueBaseName + "-dead")
	fmt.Printf("sendQueueName: %s\n", *sendQueueName)
	var deadArn *string
	p.queue.deadQueueUrl, deadArn, err = p.createQueue(ctx, deadQueueName, nil, nil)
	if err != nil {
		return err
	}
	p.queue.sendQueueUrl, _, err = p.createQueue(ctx, sendQueueName, deadArn, p.queue.maxRetry)
	if err != nil {
		return err
	}
	p.queue.receiveQueueUrl, _, err = p.createQueue(ctx, receiveQueueName, deadArn, p.queue.maxRetry)
	if err != nil {
		return err
	}

	return nil
}

func (p *providerHelper) CreatePod(ctx context.Context, pod *v1.Pod) error {
	return p.sendPod(ctx, pod, RequestTypeCreatePod)
}

func (p *providerHelper) DeletePod(ctx context.Context, pod *v1.Pod) error {
	return p.sendPod(ctx, pod, RequestTypeDeletePod)
}

func (p *providerHelper) GetPod(ctx context.Context, _, name string) (*v1.Pod, error) {
	pms, err := p.getPodModels(ctx)
	if err != nil {
		return nil, err
	}
	for _, pm := range pms {
		if pm.PodName == name {
			return pm.Pod, nil
		}
	}

	return nil, fmt.Errorf("pod was not found. pod name: %s", name)
}

func (p *providerHelper) GetPodStatus(ctx context.Context, namespace, name string) (*v1.PodStatus, error) {
	pod, err := p.GetPod(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	return &pod.Status, nil
}

func (p *providerHelper) GetPods(ctx context.Context) ([]*v1.Pod, error) {
	pms, err := p.getPodModels(ctx)
	if err != nil {
		return nil, err
	}
	pods := make([]*v1.Pod, 0, len(pms))
	for _, pm := range pms {
		pods = append(pods, pm.Pod)
	}
	return pods, nil
}

func (p *providerHelper) getPodModels(ctx context.Context) ([]PodModel, error) {
	indexName := DYNAMODB_INDEX_NAME_NODEID
	input := &dynamodb.QueryInput{
		TableName: aws.String(p.db.tableName),
		IndexName: aws.String(indexName),
		KeyConditions: map[string]*dynamodb.Condition{
			"NodeId": {
				ComparisonOperator: aws.String("EQ"),
				AttributeValueList: []*dynamodb.AttributeValue{{S: p.nodeId}},
			},
		},
	}
	res, err := p.db.client.Query(input)
	if err != nil {
		log.G(ctx).Errorf("failed to query data. %s", err.Error())
		return nil, err
	}
	podModels := make([]PodModel, 0)
	err = dynamodbattribute.UnmarshalListOfMaps(res.Items, &podModels)
	if err != nil {
		log.G(ctx).WithField("err", err).Error("failed to unmarshal data")
		return nil, err
	}
	return podModels, nil
}

func (p *providerHelper) sendPod(ctx context.Context, pod *v1.Pod, requestType RequestType) error {
	data, err := MarshalPod(pod)
	if err != nil {
		return err
	}
	return p.sendString(ctx, data, requestType)
}

func (p *providerHelper) SendPods(ctx context.Context, pods []*v1.Pod, requestType RequestType) error {
	log.G(ctx).WithField("pod num", len(pods)).Debug("sending pods")
	data, err := MarshalPods(pods)
	if err != nil {
		log.G(ctx).WithField("err", err).Error("failed to marshal pod")
		return err
	}
	return p.sendString(ctx, data, requestType)
}

func (p *providerHelper) sendString(_ context.Context, data *string, requestType RequestType) error {
	attr := map[string]*sqs.MessageAttributeValue{
		AWSMessageAttributeKeyType: &sqs.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(requestType.String()),
		},
	}
	var queueUrl *string
	switch requestType {
	case RequestTypeCreatePod, RequestTypeDeletePod:
		queueUrl = p.queue.sendQueueUrl
	case RequestTypeGetPod, RequestTypeGetPodStatus, RequestTypeGetPods:
		queueUrl = p.queue.receiveQueueUrl
	default:
		return errors.New("Not supported request type")
	}
	_, err := p.queue.client.SendMessage(&sqs.SendMessageInput{
		MessageGroupId:    p.queue.groupId,
		MessageAttributes: attr,
		MessageBody:       data,
		QueueUrl:          queueUrl,
	})
	return err
}

func (p *providerHelper) ReceiveMessageGetPodsHandler(ctx context.Context, pods []*v1.Pod) error {
	//TODO
	//		2. Need to check PutItem overwrite as expected
	//			pods lists has new status, so vk should update the pod of the same name

	// Create and update reported pods
	for _, pod := range pods {
		pm := PodModel{
			PodName: pod.GetName(),
			NodeId:  *p.nodeId,
			Pod:     pod,
		}
		av, err := dynamodbattribute.MarshalMap(pm)
		if err != nil {
			log.G(ctx).WithField("podName", pod.Name).Error("receive CreatePod")
		}

		// Create item in table Movies
		input := &dynamodb.PutItemInput{
			Item:      av,
			TableName: aws.String(p.db.tableName),
		}

		_, err = p.db.client.PutItem(input)
		if err != nil {
			log.G(ctx).WithField("err", err).Error("got error in putItem")
		}
	}

	// Delete not reported pods in dynamodb which is not included in pods list from agent.
	// Agent won't tell if it is deleted or not. It just tells what is running. So if a pod is not included,
	// the pod has been deleted.
	oldPods, err := p.getPodModels(ctx)
	if err != nil {
		log.G(ctx).WithField("err", err).Error("failed to list pods")
		return err
	}
	existingPodNames := make(map[string]bool)
	for _, pod := range pods {
		existingPodNames[pod.GetName()] = true
	}
	for _, op := range oldPods {
		if _, ok := existingPodNames[op.PodName]; !ok {
			log.G(ctx).WithField("podName", op.PodName).Debug("Change pod status to Failed (terminated)")
			op.Pod.Status.Phase = v1.PodFailed

			av, err := dynamodbattribute.MarshalMap(op)
			if err != nil {
				log.G(ctx).WithField("err", err).WithField("podName", op.PodName).Error("receive CreatePod")
			}

			input := &dynamodb.PutItemInput{
				Item:      av,
				TableName: aws.String(p.db.tableName),
			}

			_, err = p.db.client.PutItem(input)
			if err != nil {
				log.G(ctx).WithField("err", err).Error("failed to update old pod")
			}
		}
	}
	return nil
}

func (p *providerHelper) StartReceivingMessage(ctx context.Context) {
	var err error
	for {
		select {
		case <-ctx.Done():
			log.G(ctx).Info("Context is done. Stopping receiving message")
			break
		case <-time.After(time.Second):
			err = p.ReceiveMessage(ctx)
			if err != nil {
				log.G(ctx).WithField("err", err).Error("got error receiving message")
			}
		}
	}
}

func (p *providerHelper) ReceiveMessage(ctx context.Context) error {
	msgResult, err := p.queue.client.ReceiveMessage(&sqs.ReceiveMessageInput{
		AttributeNames: []*string{
			aws.String(sqs.MessageSystemAttributeNameSentTimestamp),
		},
		MessageAttributeNames: []*string{
			aws.String(sqs.QueueAttributeNameAll),
		},
		QueueUrl:            p.queue.receiveQueueUrl,
		MaxNumberOfMessages: aws.Int64(1),
		VisibilityTimeout:   p.queue.visibilityTimeout,
		WaitTimeSeconds:     p.queue.waitTimeSeconds,
	})
	if err != nil {
		return err
	}

	for _, m := range msgResult.Messages {
		requestType := StringToRequestType(m.MessageAttributes[AWSMessageAttributeKeyType].StringValue)
		switch requestType {
		case RequestTypeGetPods:
			pods, err := UnMarshalPods(m.Body)
			if err != nil {
				log.G(ctx).WithField("err", err).Error("Failed to unmarshl pods")
				continue
			}
			err = p.ReceiveMessageGetPodsHandler(ctx, pods)
			if err != nil {
				log.G(ctx).WithField("err", err).Error("Failed in get pods handler")
				continue
			}
			_, err = p.queue.client.DeleteMessage(&sqs.DeleteMessageInput{
				QueueUrl:      p.queue.receiveQueueUrl,
				ReceiptHandle: m.ReceiptHandle,
			})
			if err != nil {
				log.G(ctx).WithField("err", err).Error("Failed delete message")
				// log
			}
			// This is for agent side. i.e. in different queue
			//case RequestTypeCreatePod, RequestTypeDeletePod:
			//	pod := &v1.Pod{}
			//	err := pod.Unmarshal([]byte(*m.Body))
			//	if err != nil {
			//		continue
			//	}
			//	msgs = append(msgs, ReceivedMessage{
			//		Pod:             pod,
			//		RequestType:     requestType,
			//		DeleteMessageId: m.ReceiptHandle,
			//	})
		}
	}
	return nil
}

type ReceivedMessage struct {
	Pods            []*v1.Pod
	Pod             *v1.Pod
	Data            *string
	RequestType     RequestType
	DeleteMessageId *string
}

func GenerateNamespaceNameKey(namespace, name string) *string {
	return aws.String(strings.Join([]string{namespace, name}, SEPARATER))

}

func ReadNamespaceNameKey(key *string) (string, string, error) {
	if key == nil {
		return "", "", fmt.Errorf("key is nil")
	}
	keys := strings.Split(*key, SEPARATER)
	if len(keys) < 2 {
		return "", "", fmt.Errorf("invalid format key: %s", key)
	}
	return keys[0], keys[1], nil
}

type RequestType string

const (
	RequestTypeCreatePod    RequestType = "CreatePod"
	RequestTypeDeletePod                = "DeletePod"
	RequestTypeGetPod                   = "GetPod"
	RequestTypeGetPodStatus             = "GetPodStatus"
	RequestTypeGetPods                  = "GetPods"
	RequestTypeUnknown                  = "Unknown"
)

func (r RequestType) String() string {
	return string(r)
}
func StringToRequestType(s *string) RequestType {
	if s == nil {
		return RequestTypeUnknown
	}
	switch *s {
	case "CreatePod":
		return RequestTypeCreatePod
	case "DeletePod":
		return RequestTypeDeletePod
	case "GetPod":
		return RequestTypeGetPod
	case "GetPods":
		return RequestTypeGetPods
	case "GetPodStatus":
		return RequestTypeGetPodStatus
	default:
		return RequestTypeUnknown
	}
}

const (
	SEPARATER                  = "/"
	AWSMessageAttributeKeyType = "Type"
)

func MarshalPod(pod *v1.Pod) (*string, error) {
	// Note: data marshalled by k8s pod marshal causes sqs error.
	//		 json marshal is used.
	b, err := json.Marshal(pod)
	if err != nil {
		return nil, err
	}
	return aws.String(string(b)), nil
}

func UnMarshalPod(data *string) (*v1.Pod, error) {
	if data == nil {
		return nil, errors.New("data is nil")
	}
	pod := &v1.Pod{}
	err := json.Unmarshal([]byte(*data), pod)
	return pod, err
}

func MarshalPods(pods []*v1.Pod) (*string, error) {
	b, err := json.Marshal(pods)
	if err != nil {
		return nil, err
	}
	return aws.String(string(b)), nil
}

func UnMarshalPods(data *string) ([]*v1.Pod, error) {
	if data == nil {
		return nil, errors.New("data is nil")
	}
	pods := make([]*v1.Pod, 0)
	err := json.Unmarshal([]byte(*data), &pods)
	return pods, err
}

func (p *providerHelper) createQueue(ctx context.Context, queueName *string, deadQueueArn *string, maxRetry *int64) (queueUrl *string, arn *string, err error) {
	params := &sqs.CreateQueueInput{
		QueueName: queueName,
		Attributes: map[string]*string{
			// VisibilityTimeout:取得したメッセージは指定した秒数の間、他から見えなくする。
			sqs.QueueAttributeNameVisibilityTimeout: aws.String("30"),

			// ReceiveMessageWaitTimeSeconds: ロングポーリングの秒数。
			sqs.QueueAttributeNameReceiveMessageWaitTimeSeconds: aws.String(strconv.Itoa(int(*p.queue.waitTimeSeconds))),
		},
	}

	if deadQueueArn != nil && maxRetry != nil {
		redrivePolicy := fmt.Sprintf(
			"{\"deadLetterTargetArn\":\"%s\",\"maxReceiveCount\":%d}",
			*deadQueueArn,
			*maxRetry,
		)
		params.Attributes[sqs.QueueAttributeNameRedrivePolicy] = aws.String(redrivePolicy)
	}
	queueResp, err := p.queue.client.CreateQueueWithContext(ctx, params)
	if err != nil {
		return nil, nil, err
	}
	resp, err := p.queue.client.GetQueueAttributes(&sqs.GetQueueAttributesInput{
		QueueUrl: queueResp.QueueUrl,
		AttributeNames: []*string{
			aws.String(sqs.QueueAttributeNameAll),
		},
	})
	if err != nil {
		return nil, nil, err
	}
	arn = resp.Attributes[sqs.QueueAttributeNameQueueArn]

	return queueResp.QueueUrl, arn, nil
}
