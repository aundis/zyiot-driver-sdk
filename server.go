package zdk

import (
	"context"
	"sync"
	"time"

	"github.com/aundis/wrpc"
	"github.com/gogf/gf/v2/container/glist"
	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/gconv"
	"github.com/gorilla/websocket"
)

func NewServer() *Server {
	return &Server{
		queue:                     glist.New(true),
		deviceMap:                 map[string]*Device{},
		cond:                      sync.NewCond(new(sync.Mutex)),
		deviceOnlineStatusMap:     gmap.NewStrIntMap(true),
		serialNumberToDeviceIdMap: gmap.NewStrStrMap(true),
	}
}

type Server struct {
	cond                      *sync.Cond
	queue                     *glist.List
	clinet                    *wrpc.Client
	mutex                     sync.Mutex
	deviceMap                 map[string]*Device
	deviceOnlineStatusMap     *gmap.StrIntMap
	serialNumberToDeviceIdMap *gmap.StrStrMap
	callDeviceActionHandler   CallDeviceActionHandler
}

type CallDeviceActionHandler func(ctx context.Context, deviceId string, action string, args map[string]any) (any, error)

func (s *Server) Run(ctx context.Context, address string) {
	for {
		func() {
			conn, _, err := websocket.DefaultDialer.Dial(address, nil)
			if err != nil {
				// 连接失败, 5秒后重连
				g.Log().Error(ctx, "websocket dial error: ", err)
				time.Sleep(5 * time.Second)
				return
			}
			g.Log().Info(ctx, "driver server online")
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			client := wrpc.NewClient(conn)
			client.MustBind("deviceCreated", s.onDeviceCreated)
			client.MustBind("deviceUpdated", s.onDeviceUpdated)
			client.MustBind("deviceDeleted", s.onDeviceDeleted)
			client.MustBind("callDeviceAction", s.onCallDeviceAction)
			s.clinet = client
			// 启用消息队列
			go s.messageQueueMain(ctx, client)
			// 拉取设备数据缓存到本地
			go s.updateCacheDeviceList(ctx, client)
			// 同步设备在线状态
			go s.deviceOnlineStatusPush(ctx)
			err = client.Start(ctx)
			if err != nil {
				// 与服务器断开连接, 5秒后重连
				g.Log().Error(ctx, "driver server offline: ", err)
				time.Sleep(5 * time.Second)
				return
			}
		}()
	}
}

func (s *Server) messageQueueMain(ctx context.Context, client *wrpc.Client) {
	var err error
	for {
		select {
		case <-ctx.Done():
			return
		default:
			e := s.queue.Front()
			if e == nil {
				s.cond.L.Lock()
				s.cond.Wait()
				s.cond.L.Unlock()
				continue
			}

			switch n := e.Value.(type) {
			case DeviceOnlineMsg:
				_, err = client.Request(ctx, wrpc.RequestData{
					Command: "onlineDevice",
					Data:    n,
				})
			case DeviceOfflineMsg:
				_, err = client.Request(ctx, wrpc.RequestData{
					Command: "offlineDevice",
					Data:    n,
				})
			case DevicePropertiesReportMsg:
				_, err = client.Request(ctx, wrpc.RequestData{
					Command: "reportDeviceProperties",
					Data:    n,
				})
			default:
				g.Log().Error(ctx, "not support msg type %T", n)
			}

			if err != nil {
				g.Log().Errorf(ctx, "send %T message error: %s", e.Value, err.Error())
			}
			if ctx.Err() == context.Canceled {
				return
			}
			s.queue.PopFront()
		}
	}
}

func (s *Server) updateCacheDeviceList(ctx context.Context, client *wrpc.Client) {
	// 首次连接拉取设备数据到本地缓存
	list, err := requestDevices(ctx, client)
	if err != nil {
		g.Log().Error(ctx, "driver first pull device list error:", err.Error())
		return
	}

	g.Log().Infof(ctx, "get device list success %v", list)
	s.appendDeviceListToLocalCache(list)
}

func (s *Server) deviceOnlineStatusPush(ctx context.Context) {
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
	return s.getDeviceListFromLocalCache()
}

func (s *Server) GetDevice(id string) *Device {
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

func (s *Server) SerialNumberToDeviceId(serialNumber string) string {
	return s.serialNumberToDeviceIdMap.Get(serialNumber)
}
