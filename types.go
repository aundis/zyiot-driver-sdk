package zdk

import "time"

type DeviceOnlineMsg struct {
	DeviceIds []string  `json:"deviceIds"`
	Time      time.Time `json:"time"`
}

type DeviceOfflineMsg struct {
	DeviceIds []string  `json:"deviceIds"`
	Time      time.Time `json:"time"`
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

type ProductStatus string

const (
	ProductRelease   ProductStatus = "已发布"
	ProductUnRelease ProductStatus = "未发布"
)

type Product struct {
	Id       string        `json:"_id"`      // 主键
	Name     string        `json:"name"`     // 名字
	Number   string        `json:"number"`   // 产品标识
	Protocol string        `json:"protocol"` // 协议
	Status   ProductStatus `json:"status"`   // 产品状态
	Comment  string        `json:"comment"`  // 描述
}
