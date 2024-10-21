package zdk

import (
	"context"
	"time"

	"github.com/aundis/wrpc"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/gconv"
)

func (s *Server) updateCacheDeviceList(ctx context.Context, client *wrpc.Client) error {
	defer s.initWawit.Done()

	// 首次连接拉取设备数据到本地缓存
	list, err := requestDevices(ctx, client)
	if err != nil {
		return gerror.Newf("driver first pull device list error: %v", err.Error())
	}

	g.Log().Infof(ctx, "get device list success %v", list)
	s.appendDeviceListToLocalCache(list)
	return nil
}

func (s *Server) deviceOnlineStatusPush(ctx context.Context) {
	defer s.initWawit.Done()
	for deviceId, status := range s.deviceOnlineStatusMap.Map() {
		if status == 1 {
			s.OnlineDevice(deviceId)
			g.Log().Infof(ctx, "sync device %s online status to server", deviceId)
		} else {
			s.OfflineDevice(deviceId)
			g.Log().Infof(ctx, "sync device %s offline status to server", deviceId)
		}
	}
}

func (s *Server) appendDeviceListToLocalCache(list []Device) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, item := range list {
		s.deviceMap[item.Id] = &item
		s.serialNumberToDeviceIdMap.Set(item.SerialNumber, item.Id)
	}
}

func (s *Server) getDeviceListFromLocalCache() []Device {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var result []Device
	for _, v := range s.deviceMap {
		result = append(result, *v)
	}
	return result
}

// 从服务器中请求设备列表
func requestDevices(ctx context.Context, clinet *wrpc.Client) ([]Device, error) {
	var list []Device
	err := clinet.RequestAndUnmarshal(ctx, wrpc.RequestData{
		Command: "getDeviceList",
		Data:    nil,
	}, &list)
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Server) OnlineDevice(deviceId string) {
	defer s.cond.Broadcast()
	s.queue.PushBack(DeviceOnlineMsg{
		DeviceId: deviceId,
		Time:     time.Now(),
	})
	s.deviceOnlineStatusMap.Set(deviceId, 1)
}

func (s *Server) OfflineDevice(deviceId string) {
	defer s.cond.Broadcast()
	s.queue.PushBack(DeviceOfflineMsg{
		DeviceId: deviceId,
		Time:     time.Now(),
	})
	s.deviceOnlineStatusMap.Set(deviceId, 0)
}

func (s *Server) ReportDeviceProperties(deviceId string, data map[string]any) {
	defer s.cond.Broadcast()
	// 同时修改缓存的设备状态
	s.queue.PushBack(DevicePropertiesReportMsg{
		DeviceId: deviceId,
		Data:     data,
		Time:     time.Now(),
	})
}

func (s *Server) CreateDevice(ctx context.Context, in CreateDeviceReq) (string, error) {
	res, err := s.clinet.Request(ctx, wrpc.RequestData{
		Command: "createDevice",
		Data:    in,
	})
	if err != nil {
		return "", err
	}
	return gconv.String(res), nil
}

// 从缓存中获取数据
func (s *Server) GetDevices() []Device {
	s.initWawit.Wait()

	return s.getDeviceListFromLocalCache()
}

func (s *Server) GetDevice(id string) *Device {
	s.initWawit.Wait()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.deviceMap[id]
}

type deviceCreatedReq struct {
	Id           string       `json:"_id"`          // 主键
	SerialNumber string       `json:"serialNumber"` // 序列号
	Product      string       `json:"product"`      // 产品
	DriverName   string       `json:"driverName"`   // 驱动名称
	Name         string       `json:"name"`         // 名字
	Status       DeviceStatus `json:"status"`       // 设备状态
	Secret       string       `json:"secret"`       // 密钥
	Comment      string       `json:"comment"`      // 备注
}

func (s *Server) onDeviceCreated(ctx context.Context, req *deviceCreatedReq) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.deviceMap[req.Id] = &Device{
		Id:           req.Id,
		SerialNumber: req.SerialNumber,
		Product:      req.Product,
		DriverName:   req.DriverName,
		Name:         req.Name,
		Status:       string(req.Status),
		Secret:       req.Secret,
		Comment:      req.Comment,
	}
	s.serialNumberToDeviceIdMap.Set(req.SerialNumber, req.Id)
	g.Log().Infof(ctx, "on device created %v", req)
	return nil
}

type deviceUpdatedReq struct {
	Id           string       `json:"_id"`          // 主键
	SerialNumber string       `json:"serialNumber"` // 序列号
	Product      string       `json:"product"`      // 产品
	DriverName   string       `json:"driverName"`   // 驱动名称
	Name         string       `json:"name"`         // 名字
	Status       DeviceStatus `json:"status"`       // 设备状态
	Secret       string       `json:"secret"`       // 密钥
	Comment      string       `json:"comment"`      // 备注
}

func (s *Server) onDeviceUpdated(ctx context.Context, req *deviceUpdatedReq) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.deviceMap[req.Id] = &Device{
		Id:           req.Id,
		SerialNumber: req.SerialNumber,
		Product:      req.Product,
		DriverName:   req.DriverName,
		Name:         req.Name,
		Status:       string(req.Status),
		Secret:       req.Secret,
		Comment:      req.Comment,
	}
	s.serialNumberToDeviceIdMap.Set(req.SerialNumber, req.Id)
	g.Log().Infof(ctx, "on device updated %v", req)
	return nil
}

type deviceDeletedReq struct {
	DeviceId string `json:"deviceId"`
}

func (s *Server) onDeviceDeleted(ctx context.Context, req *deviceDeletedReq) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.deviceMap, req.DeviceId)
	// TODO: clear serial number to device id map
	g.Log().Infof(ctx, "on device deleted %v", req)
	return nil
}

type callDeviceActionReq struct {
	DeviceId string         `json:"deviceId"`
	Action   string         `json:"action"`
	Args     map[string]any `json:"args"`
}

func (s *Server) onCallDeviceAction(ctx context.Context, req *callDeviceActionReq) (interface{}, error) {
	if s.callDeviceActionHandler == nil {
		return nil, gerror.New("driver not impl call device action")
	}
	return s.callDeviceActionHandler(ctx, req.DeviceId, req.Action, req.Args)
}

func (s *Server) SetCallDeviceActionHandler(handler CallDeviceActionHandler) {
	s.callDeviceActionHandler = handler
}

type setDevicePropertiesReq struct {
	DeviceId string         `json:"deviceId"`
	Values   map[string]any `json:"values"`
}

func (s *Server) onSetDeviceProperties(ctx context.Context, req *setDevicePropertiesReq) error {
	if s.setDevicePropertiesHandler == nil {
		return gerror.New("driver not impl set device properties")
	}
	return s.setDevicePropertiesHandler(ctx, req.DeviceId, req.Values)
}

func (s *Server) SetSetDevicePropertiesHandler(handler SetDevicePropertiesHandler) {
	s.setDevicePropertiesHandler = handler
}

func (s *Server) SerialNumberToDeviceId(serialNumber string) string {
	return s.serialNumberToDeviceIdMap.Get(serialNumber)
}
