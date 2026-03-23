# ESPHome: mDNS vs Native API Discovery Comparison

## What mDNS Discovery Provides

mDNS (Multicast DNS) only provides basic device-level information via TXT records:

- **Device name** (e.g., "basement-12-edison-02")
- **IP address and hostname** (e.g., "192.168.88.69", "basement-12-edison-02.local.")
- **Port** (always 6053 for ESPHome API)
- **Basic metadata:**
  - ESPHome version (e.g., "2025.11.5")
  - MAC address
  - Board type (e.g., "generic-bk7231t-qfn32-tuya")
  - Platform (e.g., "BK7231T", "ESP8266")
  - API encryption status (e.g., "Noise_NNpsk0_25519_ChaChaPoly_SHA256")
  - Friendly name

**What mDNS CANNOT provide:**
- ❌ List of entities on the device
- ❌ Entity types (light, switch, sensor, etc.)
- ❌ Entity capabilities (supported commands, color modes, ranges)
- ❌ Current entity states (on/off, brightness, temperature)
- ❌ Entity keys (required to send commands)
- ❌ Real-time state updates

## What ESPHome Native API Provides

The ESPHome native API (TCP on port 6053) provides complete entity information:

### 1. Entity Discovery (ListEntities)

After connecting via the native API and sending `ListEntitiesRequest`, the device responds with detailed entity definitions:

**Light Entity Example:**
```protobuf
ListEntitiesLightResponse {
  key: 654929610                    # Unique entity key (REQUIRED for commands)
  object_id: "basement_12_edison_02" # Machine-readable ID
  name: "basement-12-edison-02"      # Human-readable name
  supported_color_modes: [COLOR_MODE_BRIGHTNESS]
  min_mireds: 153
  max_mireds: 500
  effects: ["effect1", "effect2"]   # Available effects
}
```

**Number Entity Example:**
```protobuf
ListEntitiesNumberResponse {
  key: 3847201956
  name: "Rolling code counter"
  min_value: 0
  max_value: 100
  step: 1
  unit_of_measurement: "%"
}
```

**Select Entity Example:**
```protobuf
ListEntitiesSelectResponse {
  key: 1029384756
  name: "Mode"
  options: ["low", "medium", "high"]  # Available options
}
```

### 2. Complete Entity Type Coverage

The ESPHome API provides detailed metadata for all 13 entity types:

| Entity Type | Key Metadata from API |
|------------|----------------------|
| **Light** | `supported_color_modes` (RGB, brightness, color_temp, etc.), `min_mireds`/`max_mireds`, `effects` list |
| **Switch** | Basic on/off capability |
| **Binary Sensor** | `device_class` (motion, door, window, etc.) |
| **Sensor** | `unit_of_measurement`, `device_class`, `accuracy_decimals` |
| **Fan** | `supports_speed`, `supported_speed_count`, `supports_oscillation` |
| **Climate** | `supported_modes` (heat/cool/auto/off), `visual_min_temperature`, `visual_max_temperature` |
| **Cover** | `supports_position`, `supports_tilt` |
| **Lock** | Command support (lock/unlock/open) |
| **Number** | `min_value`, `max_value`, `step`, `unit_of_measurement` |
| **Select** | `options` array (available choices) |
| **Button** | Basic press capability |
| **Text Sensor** | Current string value |
| **Media Player** | `supported_features` (play, pause, volume, etc.) |

### 3. Current State Values

The API provides initial state for all entities:

```protobuf
LightStateResponse {
  key: 654929610
  state: true              # Is the light on?
  brightness: 0.749020     # Brightness level (0.0-1.0)
  color_temperature: 300   # Color temperature in mireds
  red: 1.0, green: 1.0, blue: 1.0  # RGB values
}

SensorStateResponse {
  key: 1593748205
  state: 22.5              # Current temperature reading
}

SwitchStateResponse {
  key: 4028471639
  state: false             # Current switch state
}
```

### 4. Real-Time State Updates

By subscribing with `SubscribeStatesRequest`, you receive live updates whenever entity states change:

```protobuf
# When user turns on a light via physical switch or HA:
LightStateResponse { key: 654929610, state: true, brightness: 1.0 }

# When temperature sensor updates:
SensorStateResponse { key: 1593748205, state: 23.1 }
```

### 5. Command Execution

The API allows sending commands to entities using their keys:

```protobuf
# Turn light on
LightCommandRequest {
  key: 654929610
  state: true
}

# Set brightness
LightCommandRequest {
  key: 654929610
  brightness: 0.5
}

# Set RGB color
LightCommandRequest {
  key: 654929610
  red: 1.0, green: 0.0, blue: 0.0
}
```

## Why Both Are Needed

### mDNS Discovery Use Cases:
1. **Device presence detection** - Know which devices are online
2. **Network addressing** - Get IP/hostname to connect
3. **Basic device info** - Version, board, MAC (for display/logging)
4. **Encryption detection** - Know if API key is needed

### Native API Use Cases:
1. **Entity enumeration** - What entities exist on the device
2. **Capability discovery** - What commands each entity supports
3. **Command routing** - Entity keys required to send commands
4. **State synchronization** - Get current values and real-time updates
5. **Control** - Send commands to change entity states

## Summary Table

| Capability | mDNS | Native API |
|-----------|------|------------|
| Find devices on network | ✅ | ❌ |
| Get device IP/hostname | ✅ | ❌ |
| Get ESPHome version | ✅ | ❌ |
| Get MAC address | ✅ | ❌ |
| Detect encryption requirement | ✅ | ❌ |
| List entities | ❌ | ✅ |
| Get entity keys | ❌ | ✅ |
| Get entity capabilities | ❌ | ✅ |
| Get current state | ❌ | ✅ |
| Receive state updates | ❌ | ✅ |
| Send commands | ❌ | ✅ |
| Control devices | ❌ | ✅ |

## Implementation Strategy

The optimal approach uses **both** discovery methods:

1. **Phase 1: mDNS Discovery**
   ```go
   discovery := mdns.NewDiscovery()
   devices := discovery.Discover(ctx)
   // Result: List of devices with IP, name, version
   ```

2. **Phase 2: Native API Connection**
   ```go
   for _, device := range devices {
       client := client.GetClient(device.Address, encryptionKey)
       
       // Handshake
       client.Send(&api.HelloRequest{})
       client.Send(&api.ConnectRequest{})
       
       // Get entities
       client.Send(&api.ListEntitiesRequest{})
       // Receive: ListEntitiesXxxResponse for each entity
       // Receive: ListEntitiesDoneResponse
       
       // Subscribe to updates
       client.Send(&api.SubscribeStatesRequest{})
       // Receive: XxxStateResponse when states change
       
       // Store entities in system
       store.Save(entity)
   }
   ```

This two-phase approach ensures you can:
- Automatically discover new devices (mDNS)
- Get complete entity information (Native API)
- Control devices (Native API)
- Stay synchronized with state changes (Native API subscriptions)
