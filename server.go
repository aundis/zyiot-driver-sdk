package zdk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aundis/wrpc"
	"github.com/bep/debounce"
	"github.com/gogf/gf/v2/container/glist"
	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gorilla/websocket"
)

func NewServer() *Server {
	server := &Server{
		queue:                       glist.New(true),
		onlineQueue:                 glist.New(true),
		offlineQueue:                glist.New(true),
		deviceMap:                   map[string]*Device{},
		productMap:                  map[string]*Product{},
		cond:                        sync.NewCond(new(sync.Mutex)),
		deviceOnlineStatusMap:       gmap.NewStrIntMap(true),
		selfDeviceOnlineStatusMap:   gmap.NewStrIntMap(true),
		productNumberToProductIdMap: gmap.NewStrStrMap(true),
		onlineDebounce:              debounce.New(time.Second),
		offlineDebounce:             debounce.New(time.Second),
	}
	server.initWawit.Add(3)
	return server
}

type Server struct {
	name                        string
	cond                        *sync.Cond
	queue                       *glist.List
	onlineQueue                 *glist.List
	offlineQueue                *glist.List
	clinet                      *wrpc.Client
	mutex                       sync.Mutex
	deviceMap                   map[string]*Device
	productMapMutex             sync.Mutex
	productMap                  map[string]*Product
	deviceOnlineStatusMap       *gmap.StrIntMap
	selfDeviceOnlineStatusMap   *gmap.StrIntMap // 用来重连后同步本设备的在线状态
	productNumberToProductIdMap *gmap.StrStrMap
	callDeviceActionHandler     CallDeviceActionHandler
	setDevicePropertiesHandler  SetDevicePropertiesHandler
	initWawit                   sync.WaitGroup
	onlineDebounce              func(f func())
	offlineDebounce             func(f func())
}

type CallDeviceActionHandler func(ctx context.Context, deviceId string, action string, args map[string]any) (any, error)
type SetDevicePropertiesHandler func(ctx context.Context, deviceId string, values map[string]any) error

type RunOption struct {
	Address string
	Name    string
	Token   string
}

func (s *Server) Run(ctx context.Context, option RunOption) {
	s.name = option.Name
	url := fmt.Sprintf(`ws://%s/admin/objectql/once?call=driver.login&args={"name":"%s"}&QTOKEN=%s`, option.Address, option.Name, option.Token)

	for {
		func() {
			conn, _, err := websocket.DefaultDialer.Dial(url, nil)
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
			go s.updateCacheDeviceList(ctx, client)
			// 拉取产品数据缓存到本地
			go s.initProductListCache(ctx, client)
			// 同步设备在线状态
			go s.deviceOnlineStatusPush(ctx)
			defer s.initWawit.Add(3)
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
	Name     string        `json:"name"`     // 名字
	Number   string        `json:"number"`   // 产品标识
	Protocol string        `json:"protocol"` // 协议
	Status   ProductStatus `json:"status"`   // 产品状态
	Comment  string        `json:"comment"`  // 描述
}

func (s *Server) onProductCreated(ctx context.Context, req *productCreatedReq) error {
	s.productMapMutex.Lock()
	defer s.productMapMutex.Unlock()

	s.productMap[req.Number] = &Product{
		Name:     req.Name,
		Number:   req.Number,
		Protocol: req.Protocol,
		Status:   req.Status,
		Comment:  req.Comment,
	}
	g.Log().Infof(ctx, "on product created %v", req)
	return nil
}

type productUpdatedReq struct {
	Name     string        `json:"name"`     // 名字
	Number   string        `json:"number"`   // 产品标识
	Protocol string        `json:"protocol"` // 协议
	Status   ProductStatus `json:"status"`   // 产品状态
	Comment  string        `json:"comment"`  // 描述
}

func (s *Server) onProductUpdated(ctx context.Context, req *productUpdatedReq) error {
	s.productMapMutex.Lock()
	defer s.productMapMutex.Unlock()

	s.productMap[req.Number] = &Product{
		Name:     req.Name,
		Number:   req.Number,
		Protocol: req.Protocol,
		Status:   req.Status,
		Comment:  req.Comment,
	}
	g.Log().Infof(ctx, "on product updated %v", req)
	return nil
}

type productDeletedReq struct {
	Number string `json:"number" v:"required"`
}

func (s *Server) onProductDeleted(ctx context.Context, req *productDeletedReq) error {
	s.productMapMutex.Lock()
	defer s.productMapMutex.Unlock()

	delete(s.productMap, req.Number)
	// TODO: clear serial number to device id map
	g.Log().Infof(ctx, "on product deleted %v", req)
	return nil
}
