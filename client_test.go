package sarama

import (
	"io"
	"testing"
)

func safeClose(t *testing.T, c io.Closer) {
	err := c.Close()
	if err != nil {
		t.Error(err)
	}
}

func TestSimpleClient(t *testing.T) {
	seedBroker := newMockBroker(t, 1)

	seedBroker.Returns(new(MetadataResponse))

	client, err := NewClient([]string{seedBroker.Addr()}, nil)
	if err != nil {
		t.Fatal(err)
	}

	seedBroker.Close()
	safeClose(t, client)
}

func TestCachedPartitions(t *testing.T) {
	seedBroker := newMockBroker(t, 1)

	replicas := []int32{3, 1, 5}
	isr := []int32{5, 1}

	metadataResponse := new(MetadataResponse)
	metadataResponse.AddBroker("localhost:12345", 2)
	metadataResponse.AddTopicPartition("my_topic", 0, 2, replicas, isr, ErrNoError)
	metadataResponse.AddTopicPartition("my_topic", 1, 2, replicas, isr, ErrLeaderNotAvailable)
	seedBroker.Returns(metadataResponse)

	config := NewConfig()
	config.Metadata.Retry.Max = 0
	c, err := NewClient([]string{seedBroker.Addr()}, config)
	if err != nil {
		t.Fatal(err)
	}
	client := c.(*client)

	// Verify they aren't cached the same
	allP := client.cachedPartitionsResults["my_topic"][allPartitions]
	writeP := client.cachedPartitionsResults["my_topic"][writablePartitions]
	if len(allP) == len(writeP) {
		t.Fatal("Invalid lengths!")
	}

	tmp := client.cachedPartitionsResults["my_topic"]
	// Verify we actually use the cache at all!
	tmp[allPartitions] = []int32{1, 2, 3, 4}
	client.cachedPartitionsResults["my_topic"] = tmp
	if 4 != len(client.cachedPartitions("my_topic", allPartitions)) {
		t.Fatal("Not using the cache!")
	}

	seedBroker.Close()
	safeClose(t, client)
}

func TestClientDoesntCachePartitionsForTopicsWithErrors(t *testing.T) {
	seedBroker := newMockBroker(t, 1)

	replicas := []int32{seedBroker.BrokerID()}

	metadataResponse := new(MetadataResponse)
	metadataResponse.AddBroker(seedBroker.Addr(), seedBroker.BrokerID())
	metadataResponse.AddTopicPartition("my_topic", 1, replicas[0], replicas, replicas, ErrNoError)
	metadataResponse.AddTopicPartition("my_topic", 2, replicas[0], replicas, replicas, ErrNoError)
	seedBroker.Returns(metadataResponse)

	config := NewConfig()
	config.Metadata.Retry.Max = 0
	client, err := NewClient([]string{seedBroker.Addr()}, config)
	if err != nil {
		t.Fatal(err)
	}

	metadataResponse = new(MetadataResponse)
	metadataResponse.AddTopic("unknown", ErrUnknownTopicOrPartition)
	seedBroker.Returns(metadataResponse)

	partitions, err := client.Partitions("unknown")

	if err != ErrUnknownTopicOrPartition {
		t.Error("Expected ErrUnknownTopicOrPartition, found", err)
	}
	if partitions != nil {
		t.Errorf("Should return nil as partition list, found %v", partitions)
	}

	// Should still use the cache of a known topic
	partitions, err = client.Partitions("my_topic")
	if err != nil {
		t.Errorf("Expected no error, found %v", err)
	}

	metadataResponse = new(MetadataResponse)
	metadataResponse.AddTopic("unknown", ErrUnknownTopicOrPartition)
	seedBroker.Returns(metadataResponse)

	// Should not use cache for unknown topic
	partitions, err = client.Partitions("unknown")
	if err != ErrUnknownTopicOrPartition {
		t.Error("Expected ErrUnknownTopicOrPartition, found", err)
	}
	if partitions != nil {
		t.Errorf("Should return nil as partition list, found %v", partitions)
	}

	seedBroker.Close()
	safeClose(t, client)
}

func TestClientSeedBrokers(t *testing.T) {
	seedBroker := newMockBroker(t, 1)

	metadataResponse := new(MetadataResponse)
	metadataResponse.AddBroker("localhost:12345", 2)
	seedBroker.Returns(metadataResponse)

	client, err := NewClient([]string{seedBroker.Addr()}, nil)
	if err != nil {
		t.Fatal(err)
	}

	seedBroker.Close()
	safeClose(t, client)
}

func TestClientMetadata(t *testing.T) {
	seedBroker := newMockBroker(t, 1)
	leader := newMockBroker(t, 5)

	replicas := []int32{3, 1, 5}
	isr := []int32{5, 1}

	metadataResponse := new(MetadataResponse)
	metadataResponse.AddBroker(leader.Addr(), leader.BrokerID())
	metadataResponse.AddTopicPartition("my_topic", 0, leader.BrokerID(), replicas, isr, ErrNoError)
	metadataResponse.AddTopicPartition("my_topic", 1, leader.BrokerID(), replicas, isr, ErrLeaderNotAvailable)
	seedBroker.Returns(metadataResponse)

	config := NewConfig()
	config.Metadata.Retry.Max = 0
	client, err := NewClient([]string{seedBroker.Addr()}, config)
	if err != nil {
		t.Fatal(err)
	}

	topics, err := client.Topics()
	if err != nil {
		t.Error(err)
	} else if len(topics) != 1 || topics[0] != "my_topic" {
		t.Error("Client returned incorrect topics:", topics)
	}

	parts, err := client.Partitions("my_topic")
	if err != nil {
		t.Error(err)
	} else if len(parts) != 2 || parts[0] != 0 || parts[1] != 1 {
		t.Error("Client returned incorrect partitions for my_topic:", parts)
	}

	parts, err = client.WritablePartitions("my_topic")
	if err != nil {
		t.Error(err)
	} else if len(parts) != 1 || parts[0] != 0 {
		t.Error("Client returned incorrect writable partitions for my_topic:", parts)
	}

	tst, err := client.Leader("my_topic", 0)
	if err != nil {
		t.Error(err)
	} else if tst.ID() != 5 {
		t.Error("Leader for my_topic had incorrect ID.")
	}

	replicas, err = client.Replicas("my_topic", 0)
	if err != nil {
		t.Error(err)
	} else if replicas[0] != 1 {
		t.Error("Incorrect (or unsorted) replica")
	} else if replicas[1] != 3 {
		t.Error("Incorrect (or unsorted) replica")
	} else if replicas[2] != 5 {
		t.Error("Incorrect (or unsorted) replica")
	}

	leader.Close()
	seedBroker.Close()
	safeClose(t, client)
}

func TestClientRefreshBehaviour(t *testing.T) {
	seedBroker := newMockBroker(t, 1)
	leader := newMockBroker(t, 5)

	metadataResponse1 := new(MetadataResponse)
	metadataResponse1.AddBroker(leader.Addr(), leader.BrokerID())
	seedBroker.Returns(metadataResponse1)

	metadataResponse2 := new(MetadataResponse)
	metadataResponse2.AddTopicPartition("my_topic", 0xb, leader.BrokerID(), nil, nil, ErrNoError)
	seedBroker.Returns(metadataResponse2)

	c, err := NewClient([]string{seedBroker.Addr()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := c.(*client)

	parts, err := client.Partitions("my_topic")
	if err != nil {
		t.Error(err)
	} else if len(parts) != 1 || parts[0] != 0xb {
		t.Error("Client returned incorrect partitions for my_topic:", parts)
	}

	tst, err := client.Leader("my_topic", 0xb)
	if err != nil {
		t.Error(err)
	} else if tst.ID() != 5 {
		t.Error("Leader for my_topic had incorrect ID.")
	}

	leader.Close()
	seedBroker.Close()
	safeClose(t, client)
}
