package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	"github.com/livekit/psrpc/examples/pubsub/protocol/broadcast"
	gproto "google.golang.org/protobuf/proto"
)

type KafkaBridgeConfig struct {
	Enabled    bool
	Brokers    []string
	Topic      string
	Partitions int32
}

type KafkaBridge struct {
	cfg      KafkaBridgeConfig
	producer sarama.AsyncProducer
	consumer sarama.Consumer
	server   *PushManagerServer
	closeWG  sync.WaitGroup
}

func NewKafkaBridge(cfg KafkaBridgeConfig, server *PushManagerServer) (*KafkaBridge, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka brokers is empty")
	}
	if cfg.Topic == "" {
		cfg.Topic = "pubsub-broadcast-topic"
	}
	if cfg.Partitions <= 0 {
		cfg.Partitions = 1
	}

	config := sarama.NewConfig()
	config.Version = sarama.V1_1_0_0
	config.ClientID = server.managerID
	config.Producer.RequiredAcks = sarama.WaitForLocal
	config.Producer.Return.Successes = true
	config.Producer.Return.Errors = true
	config.Producer.Partitioner = sarama.NewHashPartitioner
	config.ChannelBufferSize = 65536
	config.Consumer.Return.Errors = true

	if err := ensureKafkaTopic(cfg.Brokers, cfg.Topic, cfg.Partitions, config); err != nil {
		return nil, err
	}

	producer, err := sarama.NewAsyncProducer(cfg.Brokers, config)
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}
	consumer, err := sarama.NewConsumer(cfg.Brokers, config)
	if err != nil {
		producer.Close()
		return nil, fmt.Errorf("create kafka consumer: %w", err)
	}

	b := &KafkaBridge{cfg: cfg, producer: producer, consumer: consumer, server: server}
	b.closeWG.Add(2)
	go b.drainProducerSuccesses()
	go b.drainProducerErrors()
	return b, nil
}

func ensureKafkaTopic(brokers []string, topic string, partitions int32, cfg *sarama.Config) error {
	admin, err := sarama.NewClusterAdmin(brokers, cfg)
	if err != nil {
		return fmt.Errorf("create kafka admin: %w", err)
	}
	defer admin.Close()

	err = admin.CreateTopic(topic, &sarama.TopicDetail{NumPartitions: partitions, ReplicationFactor: 1}, false)
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "Topic with this name already exists") || strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return fmt.Errorf("create kafka topic %s: %w", topic, err)
}

func (b *KafkaBridge) Start(ctx context.Context) error {
	partitions, err := b.consumer.Partitions(b.cfg.Topic)
	if err != nil {
		return fmt.Errorf("get kafka partitions: %w", err)
	}
	for _, partition := range partitions {
		pc, err := b.consumer.ConsumePartition(b.cfg.Topic, partition, sarama.OffsetNewest)
		if err != nil {
			return fmt.Errorf("consume partition %d: %w", partition, err)
		}
		b.closeWG.Add(1)
		go b.consumePartition(ctx, pc)
	}
	log.Printf("[KafkaBridge] started brokers=%v topic=%s partitions=%v", b.cfg.Brokers, b.cfg.Topic, partitions)
	return nil
}

func (b *KafkaBridge) Publish(req *broadcast.BroadCastReq) error {
	if req == nil || req.Proto == nil {
		return errors.New("nil broadcast request")
	}
	payload, err := gproto.Marshal(req)
	if err != nil {
		return err
	}
	key := req.Proto.GetRoomid()
	if key == "" {
		key = req.Proto.GetUserid()
	}
	msg := &sarama.ProducerMessage{Topic: b.cfg.Topic, Value: sarama.ByteEncoder(payload)}
	if key != "" {
		msg.Key = sarama.StringEncoder(key)
	}
	select {
	case b.producer.Input() <- msg:
		return nil
	case <-time.After(3 * time.Second):
		return errors.New("kafka producer input timeout")
	}
}

func (b *KafkaBridge) Close() {
	if b == nil {
		return
	}
	_ = b.producer.Close()
	_ = b.consumer.Close()
	b.closeWG.Wait()
}

func (b *KafkaBridge) Enabled() bool { return b != nil }

func (b *KafkaBridge) drainProducerSuccesses() {
	defer b.closeWG.Done()
	for range b.producer.Successes() {
	}
}

func (b *KafkaBridge) drainProducerErrors() {
	defer b.closeWG.Done()
	for err := range b.producer.Errors() {
		if err != nil {
			log.Printf("[KafkaBridge] producer error: %v", err)
		}
	}
}

func (b *KafkaBridge) consumePartition(ctx context.Context, pc sarama.PartitionConsumer) {
	defer b.closeWG.Done()
	defer pc.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-pc.Errors():
			if !ok {
				return
			}
			log.Printf("[KafkaBridge] consumer error: %v", err)
		case msg, ok := <-pc.Messages():
			if !ok {
				return
			}
			var req broadcast.BroadCastReq
			if err := gproto.Unmarshal(msg.Value, &req); err != nil {
				log.Printf("[KafkaBridge] unmarshal error topic=%s partition=%d offset=%d err=%v", msg.Topic, msg.Partition, msg.Offset, err)
				continue
			}
			b.server.EnqueueKafkaBroadcastMsg(&req)
		}
	}
}
