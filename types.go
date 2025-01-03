package zdk

import "time"

type DeviceOnlineMsg struct {
	Numbers []string  `json:"numbers"`
	Time    time.Time `json:"time"`
}

type DeviceOfflineMsg struct {
	Numbers []string  `json:"numbers"`
	Time    time.Time `json:"time"`
}

type DevicePropertiesReportMsg struct {
	Number string         `json:"number"`
	Data   map[string]any `json:"data"`
	Time   time.Time      `json:"time"`
}

type Device struct {
	Number        string `json:"number"`        // 设备编号
	ProductNumber string `json:"productNumber"` // 产品
	DriverName    string `json:"driverName"`    // 驱动名称
	Name          string `json:"name"`          // 名字
	Status        string `json:"status"`        // 设备状态
	Comment       string `json:"comment"`       // 备注
}

type CreateDeviceReq struct {
	ProductNumber string `json:"productNumber"` // 产品
	Number        string `json:"number"`        // 设备编号
	Name          string `json:"name"`          // 名字
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
	Name     string        `json:"name"`     // 名字
	Number   string        `json:"number"`   // 产品标识
	Protocol string        `json:"protocol"` // 协议
	Status   ProductStatus `json:"status"`   // 产品状态
	Comment  string        `json:"comment"`  // 描述
}
