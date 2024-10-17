package zdk

import "time"

type DeviceOnlineMsg struct {
	DeviceId string    `json:"deviceId"`
	Time     time.Time `json:"time"`
}

type DeviceOfflineMsg struct {
	DeviceId string    `json:"deviceId"`
	Time     time.Time `json:"time"`
}

type DevicePropertiesReportMsg struct {
	DeviceId string         `json:"deviceId"`
	Data     map[string]any `json:"data"`
	Time     time.Time      `json:"time"`
}

type Device struct {
	Id           string `json:"_id"`          // 主键
	SerialNumber string `json:"serialNumber"` // 序列号
	Product      string `json:"product"`      // 产品
	DriverName   string `json:"driverName"`   // 驱动名称
	Name         string `json:"name"`         // 名字
	Status       string `json:"status"`       // 设备状态
	Secret       string `json:"secret"`       // 密钥
	Comment      string `json:"comment"`      // 备注
}

type CreateDeviceReq struct {
	ProductNumber string `json:"productNumber"` // 产品
	Name          string `json:"name"`          // 名字
	SerialNumber  string `json:"serialNumber"`  // 序列号
	Comment       string `json:"comment"`       // 备注
}

type DeviceStatus string

const (
	DeviceStatusOnline  DeviceStatus = "在线"
	DeviceStatusOffline DeviceStatus = "离线"
)
