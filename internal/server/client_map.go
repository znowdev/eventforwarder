package server

import "sync"

type clientMap struct {
	clients map[string]struct{}
	mux     sync.Mutex
}

func (cm *clientMap) AddClient(clientId string) {
	cm.mux.Lock()
	defer cm.mux.Unlock()
	cm.clients[clientId] = struct{}{}
}

func (cm *clientMap) HasClient(clientId string) bool {
	cm.mux.Lock()
	defer cm.mux.Unlock()
	_, ok := cm.clients[clientId]
	return ok

}

func (cm *clientMap) RemoveClient(clientId string) {
	cm.mux.Lock()
	defer cm.mux.Unlock()
	delete(cm.clients, clientId)
}

func (cm *clientMap) Clients() []string {
	cm.mux.Lock()
	defer cm.mux.Unlock()
	clients := make([]string, 0, len(cm.clients))
	for client := range cm.clients {
		clients = append(clients, client)
	}
	return clients
}

func (cm *clientMap) ConnectedClientsNo() int {
	cm.mux.Lock()
	defer cm.mux.Unlock()
	return len(cm.clients)
}
