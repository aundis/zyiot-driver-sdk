package zdk

import (
	"context"
	"sync"
	"time"

	"github.com/aundis/wrpc"
	"github.com/gogf/gf/v2/container/glist"
	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gorilla/websocket"
)

func NewServer() *Server {
	server := &Server{
		queue:                       glist.New(true),
		deviceMap:                   map[string]*Device{},
		productMap:                  map[string]*ProductFull{},
		cond:                        sync.NewCond(new(sync.Mutex)),
		deviceOnlineStatusMap:       gmap.NewStrIntMap(true),
		serialNumberToDeviceIdMap:   gmap.NewStrStrMap(true),
		productNumberToProductIdMap: gmap.NewStrStrMap(true),
	}
	return server
}

type Server struct {
	cond                        *sync.Cond
	queue                       *glist.List
	clinet                      *wrpc.Client
	mutex                       sync.Mutex
	deviceMap                   map[string]*Device
	productMapMutex             sync.Mutex
	productMap                  map[string]*ProductFull
	deviceOnlineStatusMap       *gmap.StrIntMap
	serialNumberToDeviceIdMap   *gmap.StrStrMap
	productNumberToProductIdMap *gmap.StrStrMap
	callDeviceActionHandler     CallDeviceActionHandler
	setDevicePropertiesHandler  SetDevicePropertiesHandler
	initWawit                   sync.WaitGroup
}

type CallDeviceActionHandler func(ctx context.Context, deviceId string, action string, args map[string]any) (any, error)
type SetDevicePropertiesHandler func(ctx context.Context, deviceId string, values map[string]any) error

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
			client.MustBind("setDeviceProperties", s.onSetDeviceProperties)
			client.MustBind("productCreated", s.onProductCreated)
			client.MustBind("productUpdated", s.onProductUpdated)
			client.MustBind("productDeleted", s.onProductDeleted)
			s.clinet = client
			// 启用消息队列
			go s.messageQueueMain(ctx, client)
			// 拉取设备数据缓存到本地
			s.initWawit.Add(1)
			go s.updateCacheDeviceList(ctx, client)
			// 拉取产品数据缓存到本地
			s.initWawit.Add(1)
			go s.initProductListCache(ctx, client)
			// 同步设备在线状态
			s.initWawit.Add(1)
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

func (s *Server) WaitInit() {
	s.initWawit.Wait()
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

type productCreatedReq struct {
	Id         string            `json:"_id"`        // 主键
	Name       string            `json:"name"`       // 名字
	Number     string            `json:"number"`     // 产品标识
	Protocol   string            `json:"protocol"`   // 协议
	Status     ProductStatus     `json:"status"`     // 产品状态
	Comment    string            `json:"comment"`    // 描述
	Properties []ProductProperty `json:"properties"` // 属性
	Events     []ProductEvent    `json:"events"`     // 事件
	Actions    []ProductAction   `json:"actions"`    // 动作
}

func (s *Server) onProductCreated(ctx context.Context, req *productCreatedReq) error {
	s.productMapMutex.Lock()
	defer s.productMapMutex.Unlock()

	s.productMap[req.Id] = &ProductFull{
		Id:         req.Id,
		Name:       req.Name,
		Number:     req.Number,
		Protocol:   req.Protocol,
		Status:     req.Status,
		Comment:    req.Comment,
		Properties: req.Properties,
		Events:     req.Events,
		Actions:    req.Actions,
	}
	s.serialNumberToDeviceIdMap.Set(req.Number, req.Id)
	g.Log().Infof(ctx, "on product created %v", req)
	return nil
}

type productUpdatedReq struct {
	Id         string            `json:"_id"`        // 主键
	Name       string            `json:"name"`       // 名字
	Number     string            `json:"number"`     // 产品标识
	Protocol   string            `json:"protocol"`   // 协议
	Status     ProductStatus     `json:"status"`     // 产品状态
	Comment    string            `json:"comment"`    // 描述
	Properties []ProductProperty `json:"properties"` // 属性
	Events     []ProductEvent    `json:"events"`     // 事件
	Actions    []ProductAction   `json:"actions"`    // 动作
}

func (s *Server) onProductUpdated(ctx context.Context, req *productUpdatedReq) error {
	s.productMapMutex.Lock()
	defer s.productMapMutex.Unlock()

	s.productMap[req.Id] = &ProductFull{
		Id:         req.Id,
		Name:       req.Name,
		Number:     req.Number,
		Protocol:   req.Protocol,
		Status:     req.Status,
		Comment:    req.Comment,
		Properties: req.Properties,
		Events:     req.Events,
		Actions:    req.Actions,
	}
	s.productNumberToProductIdMap.Set(req.Number, req.Id)
	g.Log().Infof(ctx, "on product updated %v", req)
	return nil
}

type productDeletedReq struct {
	ProductId string `json:"productId"`
}

func (s *Server) onProductDeleted(ctx context.Context, req *productDeletedReq) error {
	s.productMapMutex.Lock()
	defer s.productMapMutex.Unlock()

	delete(s.productMap, req.ProductId)
	// TODO: clear serial number to device id map
	g.Log().Infof(ctx, "on product deleted %v", req)
	return nil
}
