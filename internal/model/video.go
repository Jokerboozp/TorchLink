package model

import "errors"

// Camera live module records. Basic camera metadata stays in
// VideoCameraMapping; live access settings, encrypted credentials and play
// sessions are stored separately so editing a camera's name or location can
// never overwrite live configuration, and metadata APIs never see secrets.

// VideoModuleState is the platform-wide business switch of the live module.
type VideoModuleState struct {
	Enabled   bool   `json:"enabled"`
	UpdatedBy string `json:"updatedBy,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

// CameraLiveConfig is the per-camera live access configuration, keyed by
// tenantId + cameraId. Stream URLs never contain credentials.
type CameraLiveConfig struct {
	TenantID         string `json:"tenantId"`
	CameraID         string `json:"cameraId"`
	Enabled          bool   `json:"enabled"`
	AccessMode       string `json:"accessMode"`
	BrandTemplate    string `json:"brandTemplate"`
	Host             string `json:"host,omitempty"`
	RTSPPort         int    `json:"rtspPort,omitempty"`
	ONVIFPort        int    `json:"onvifPort,omitempty"`
	Channel          int    `json:"channel,omitempty"`
	NVR              bool   `json:"nvr,omitempty"`
	MainProfileToken string `json:"mainProfileToken,omitempty"`
	SubProfileToken  string `json:"subProfileToken,omitempty"`
	ManualURL        bool   `json:"manualUrl,omitempty"`
	MainStreamURL    string `json:"mainStreamUrl,omitempty"`
	SubStreamURL     string `json:"subStreamUrl,omitempty"`
	DefaultStream    string `json:"defaultStream"`
	TranscodeMode    string `json:"transcodeMode"`
	TranscodeProfile string `json:"transcodeProfile,omitempty"`
	SourceBFrames    bool   `json:"sourceBFrames,omitempty"`
	MediaNodeID      string `json:"mediaNodeId"`
	// GB28181 access: the registered device and its catalog channel.
	GBDeviceID     string          `json:"gbDeviceId,omitempty"`
	GBChannelID    string          `json:"gbChannelId,omitempty"`
	Username       string          `json:"username,omitempty"`
	HasPassword    bool            `json:"hasPassword"`
	LastTest       *CameraLiveTest `json:"lastTest,omitempty"`
	LastPlayableAt int64           `json:"lastPlayableAt,omitempty"`
	UpdatedAt      int64           `json:"updatedAt"`
}

// CameraLiveTest is a sanitized connection test result.
type CameraLiveTest struct {
	Status        string  `json:"status"`
	Message       string  `json:"message"`
	Stream        string  `json:"stream,omitempty"`
	VideoCodec    string  `json:"videoCodec,omitempty"`
	H264Profile   string  `json:"h264Profile,omitempty"`
	AudioCodec    string  `json:"audioCodec,omitempty"`
	Width         int     `json:"width,omitempty"`
	Height        int     `json:"height,omitempty"`
	FPS           float64 `json:"fps,omitempty"`
	MediaVerified bool    `json:"mediaVerified"`
	NeedTranscode bool    `json:"needTranscode,omitempty"`
	TestedAt      int64   `json:"testedAt"`
}

// CameraCredential is an AEAD-sealed {username,password} blob. KeyID
// identifies the server-side key; the key itself is never stored.
type CameraCredential struct {
	TenantID   string `json:"tenantId"`
	CameraID   string `json:"cameraId"`
	KeyID      string `json:"keyId"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
	UpdatedAt  int64  `json:"updatedAt"`
}

// VideoPlaySession is one viewer's lease on a media stream. Only a hash of
// the play token is stored.
type VideoPlaySession struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	CameraID    string `json:"cameraId"`
	Username    string `json:"username"`
	ManagedUser bool   `json:"managedUser"`
	// SessionVersion is the login session generation of a managed user, so a
	// password reset or account edit also ends live playback.
	SessionVersion int64  `json:"sessionVersion,omitempty"`
	Stream         string `json:"stream"`
	Profile        string `json:"profile"`
	App            string `json:"app"`
	StreamKey      string `json:"streamKey"`
	TokenHash      string `json:"tokenHash"`
	CreatedAt      int64  `json:"createdAt"`
	ExpiresAt      int64  `json:"expiresAt"`
	MaxExpiresAt   int64  `json:"maxExpiresAt"`
	RevokedAt      int64  `json:"revokedAt,omitempty"`
	RevokeReason   string `json:"revokeReason,omitempty"`
}

// ErrGBDeviceTaken means a GB/T 28181 device ID already belongs to another
// tenant. Device IDs are SIP identities and must map to exactly one tenant.
var ErrGBDeviceTaken = errors.New("国标设备编号已被其他租户使用")

// GBDevice is a GB/T 28181 device (IPC, NVR or lower-level platform) that
// registers to the platform's SIP server. Administrators own the settings;
// State is written by the signalling server.
type GBDevice struct {
	TenantID string `json:"tenantId"`
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	// StreamTransport is how the device sends RTP: UDP, or TCP (the device
	// connects to the media server).
	StreamTransport string        `json:"streamTransport"`
	HasPassword     bool          `json:"hasPassword"`
	Online          bool          `json:"online"`
	State           GBDeviceState `json:"state"`
	CreatedAt       int64         `json:"createdAt"`
	UpdatedAt       int64         `json:"updatedAt"`
	// Password is the sealed registration password; never serialized.
	Password *CameraCredential `json:"-"`
}

// GBDeviceState is what the device reported through SIP.
type GBDeviceState struct {
	Transport    string      `json:"transport,omitempty"`
	RemoteAddr   string      `json:"remoteAddr,omitempty"`
	RegisteredAt int64       `json:"registeredAt,omitempty"`
	ExpiresAt    int64       `json:"expiresAt,omitempty"`
	LastSeenAt   int64       `json:"lastSeenAt,omitempty"`
	UserAgent    string      `json:"userAgent,omitempty"`
	Manufacturer string      `json:"manufacturer,omitempty"`
	Model        string      `json:"model,omitempty"`
	Firmware     string      `json:"firmware,omitempty"`
	Channels     []GBChannel `json:"channels,omitempty"`
	CatalogAt    int64       `json:"catalogAt,omitempty"`
}

// GBChannel is one catalog entry of a device.
type GBChannel struct {
	ChannelID    string `json:"channelId"`
	Name         string `json:"name,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	Status       string `json:"status,omitempty"`
	ParentID     string `json:"parentId,omitempty"`
}
