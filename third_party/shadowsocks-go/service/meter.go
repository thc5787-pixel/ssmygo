package service

// TrafficMeter receives payload byte counts as they are relayed (ssmygo
// patch A4). up is client → target, down is target → client.
type TrafficMeter interface{ Add(up, down uint64) }
