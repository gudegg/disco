package handlers

import "testing"

func TestSSEManagerSubscribeBroadcastAndUnsubscribe(t *testing.T) {
	manager := NewSSEManagerImpl()

	ch := manager.Subscribe("order-service", "prod", "10.0.0.1")
	manager.BroadcastConfigChange("order-service", "prod", 2)

	select {
	case msg := <-ch:
		expected := `{"type":"config_changed","service":"order-service","env":"prod","version":2}`
		if msg != expected {
			t.Fatalf("BroadcastConfigChange() message = %q, want %q", msg, expected)
		}
	default:
		t.Fatalf("expected config change message")
	}

	manager.Unsubscribe("order-service", "prod", ch)
	manager.BroadcastHeartbeat()

	select {
	case msg := <-ch:
		t.Fatalf("received message after unsubscribe: %q", msg)
	default:
	}
}

func TestSSEManagerListConnections(t *testing.T) {
	manager := NewSSEManagerImpl()

	ch1 := manager.Subscribe("order-service", "prod", "10.0.0.1")
	ch2 := manager.Subscribe("order-service", "prod", "10.0.0.1")
	ch3 := manager.Subscribe("order-service", "prod", "10.0.0.2")

	manager.BroadcastHeartbeat()

	connections := manager.ListConnections("order-service", "prod")
	if len(connections) != 2 {
		t.Fatalf("ListConnections() len = %d, want 2", len(connections))
	}

	if connections[0].IP != "10.0.0.2" && connections[0].IP != "10.0.0.1" {
		t.Fatalf("unexpected ip %q", connections[0].IP)
	}

	var firstIP *ConnectionSnapshot
	for i := range connections {
		if connections[i].IP == "10.0.0.1" {
			firstIP = &connections[i]
			break
		}
	}
	if firstIP == nil {
		t.Fatalf("expected aggregated record for 10.0.0.1")
	}
	if firstIP.ActiveConnections != 2 {
		t.Fatalf("ActiveConnections = %d, want 2", firstIP.ActiveConnections)
	}

	manager.Unsubscribe("order-service", "prod", ch1)
	manager.Unsubscribe("order-service", "prod", ch2)
	manager.Unsubscribe("order-service", "prod", ch3)

	if got := manager.ListConnections("order-service", "prod"); len(got) != 0 {
		t.Fatalf("ListConnections() after unsubscribe len = %d, want 0", len(got))
	}
}

func TestSSEManagerBroadcastChannelFull(t *testing.T) {
	manager := NewSSEManagerImpl()
	ch := manager.Subscribe("order-service", "prod", "10.0.0.1")
	defer manager.Unsubscribe("order-service", "prod", ch)

	// channel buffer size 10; try writing 11 events via broadcast
	for i := 0; i < 11; i++ {
		manager.BroadcastConfigChange("order-service", "prod", i+1)
	}

	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}

done:
	if count != 10 {
		t.Fatalf("expected 10 messages in buffered channel, got %d", count)
	}
}
