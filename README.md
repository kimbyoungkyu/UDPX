# UDPX

UDPX는 UDP 기반 네트워크 기능들을 위한 경량 고성능 UDP 네트워킹 툴킷입니다.

UDP Relay, Multicast Forwarding, Multicast Bridge 등 다양한 UDP 패킷 전달 기능을 제공하며, 실시간 시뮬레이션, 로봇, 디지털 트윈, 자율주행, 텔레메트리 환경을 위해 설계되었습니다.

UDPX의 핵심 목표는 UDP payload를 수정하지 않고 그대로 전달하는 것입니다.

---

## Features

- UDP Unicast Relay
- UDP to Multicast Forwarding
- Multicast to UDP Bridge
- Raw Payload Forwarding
- Zero Payload Modification
- High Packet Throughput
- Low Latency
- Simple Deployment
- Windows / Linux Support

---

## Components

### UDPRelay.go

`UDPRelay.go`는 UDP 포트로 들어온 패킷을 그대로 multicast group으로 전달합니다.

```text
UDP Listener
    ↓
UDPRelay
    ↓
UDP Multicast Group
```

사용 목적:

- 외부 UDP 입력을 내부 multicast로 배포
- 센서 데이터 multicast 송출
- 시뮬레이터 UDP 패킷 분배
- payload 수정 없는 단순 relay

---

### UDPMulticastBridge.go

`UDPMulticastBridge.go`는 multicast로 수신한 패킷을 remote UDP listener로 전달하고, remote UDP listener에서 받은 패킷을 다시 multicast로 송출하는 bridge입니다.

```text
LAN A Multicast
    ↓
UDPMulticastBridge
    ↓ UDP Unicast
WAN / Internet
    ↓ UDP Unicast
UDPMulticastBridge
    ↓
LAN B Multicast
```

사용 목적:

- 서로 다른 LAN 간 multicast 효과 구현
- multicast over UDP unicast
- 공유기/NAT 환경에서 multicast-like 통신 구성
- payload 수정 없는 multicast bridge

---

## Requirements

- Go 1.22 이상 권장
- Windows / Linux
- Multicast 사용 가능한 네트워크 환경

---

## Setup

```bash
go mod init udpx
go get golang.org/x/net/ipv4
```

또는 이미 `go.mod`가 있다면:

```bash
go mod tidy
```

---

## Build

### UDPRelay.go

Windows:

```bash
go build -o UDPRelay.exe UDPRelay.go
```

Linux/macOS:

```bash
go build -o UDPRelay UDPRelay.go
```

---

### UDPMulticastBridge.go

Windows:

```bash
go build -o UDPMulticastBridge.exe UDPMulticastBridge.go
```

Linux/macOS:

```bash
go build -o UDPMulticastBridge UDPMulticastBridge.go
```

---

## Usage

## 1. UDPRelay.go

UDP로 받은 payload를 그대로 multicast group으로 전달합니다.

실행:

```bash
go run UDPRelay.go -listen 15000 -mgroup 239.10.0.1 -mport 5000
```

또는:

```bash
UDPRelay.exe -listen 15000 -mgroup 239.10.0.1 -mport 5000
```

옵션:

| Option | Description | Default |
|---|---|---|
| `-listen` | UDP listen port | `15000` |
| `-mgroup` | Multicast group address | `239.10.0.1` |
| `-mport` | Multicast port | `5000` |
| `-ttl` | Multicast TTL | `1` |

예:

```bash
UDPRelay.exe -listen 15000 -mgroup 239.10.0.1 -mport 5000 -ttl 1
```

동작:

```text
UDP 0.0.0.0:15000 수신
→ payload 그대로 유지
→ 239.10.0.1:5000 으로 multicast 송신
```

---

## 2. UDPMulticastBridge.go

서로 다른 네트워크의 multicast group을 UDP unicast로 연결합니다.

### LAN A

```bash
UDPMulticastBridge.exe -listen 15000 -mgroup 239.10.0.1 -mport 5000 -remotes "LAN_B_PUBLIC_IP:15000"
```

### LAN B

```bash
UDPMulticastBridge.exe -listen 15000 -mgroup 239.10.0.1 -mport 5000 -remotes "LAN_A_PUBLIC_IP:15000"
```

예:

```bash
UDPMulticastBridge.exe -listen 15000 -mgroup 239.10.0.1 -mport 5000 -remotes "203.0.113.20:15000"
```

여러 remote peer를 지정할 수도 있습니다.

```bash
UDPMulticastBridge.exe -listen 15000 -mgroup 239.10.0.1 -mport 5000 -remotes "203.0.113.20:15000,203.0.113.21:15000"
```

옵션:

| Option | Description | Default |
|---|---|---|
| `-listen` | UDP listener port | `15000` |
| `-mgroup` | Multicast group address | `239.10.0.1` |
| `-mport` | Multicast port | `5000` |
| `-remotes` | Remote UDP listener list | empty |
| `-iface` | Network interface name | empty |
| `-loopttl` | Payload hash based loop guard TTL | `300ms` |

동작:

```text
Remote UDP 수신
→ payload 그대로 multicast 송신

Local multicast 수신
→ payload 그대로 remote UDP peer에게 송신
```

payload는 수정하지 않습니다.

---

## Test

### UDP 송신 테스트

Windows PowerShell:

```powershell
"hello udpx" | ncat -u 127.0.0.1 15000
```

또는:

```powershell
echo hello udpx | ncat -u 127.0.0.1 15000
```

---

### Multicast 수신 확인

Wireshark filter:

```text
udp.port == 5000
```

또는 VLC 등에서:

```text
udp://@239.10.0.1:5000
```

---

## Network Notes

서로 다른 공유기 또는 외부망에서 `UDPMulticastBridge.go`를 사용할 경우, 각 공유기에서 UDP listen port를 포트포워딩해야 합니다.

예:

```text
UDP 15000 → bridge 실행 PC의 내부 IP:15000
```

Windows 방화벽에서도 해당 UDP 포트를 허용해야 합니다.

---

## Design Philosophy

UDPX는 빠르고 단순한 UDP 패킷 전달에 집중합니다.

- Payload 수정 없음
- Header 추가 없음
- Protocol 강제 없음
- 불필요한 middleware 없음
- 최소 오버헤드
- 실시간 처리 중심 설계

UDPX는 UDP 패킷을 가능한 한 빠르고 단순하게 이동시키는 것을 목표로 합니다.
