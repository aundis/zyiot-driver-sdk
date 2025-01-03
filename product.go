package zdk

import (
	"context"

	"github.com/aundis/wrpc"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

func (s *Server) initProductListCache(ctx context.Context, client *wrpc.Client) error {
	defer s.initWawit.Done()

	// 首次连接拉取设备数据到本地缓存
	list, err := requestProductList(ctx, client)
	if err != nil {
		return gerror.Newf("driver first pull device list error: %v", err.Error())
	}

	g.Log().Infof(ctx, "get product list success %v", list)
	s.resetProductListCache(list)
	return nil
}

// 从服务器中请求设备列表
func requestProductList(ctx context.Context, clinet *wrpc.Client) ([]Product, error) {
	var list []Product
	err := clinet.RequestAndUnmarshal(ctx, wrpc.RequestData{
		Command: "getProductList",
		Data: map[string]any{
			"filter": map[string]any{
				"status": "已发布",
			},
		},
	}, &list)
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Server) resetProductListCache(list []Product) {
	s.productMapMutex.Lock()
	defer s.productMapMutex.Unlock()

	for _, item := range list {
		temp := item
		s.productMap[item.Number] = &temp
		s.productNumberToProductIdMap.Set(item.Number, item.Number)
	}
}

func (s *Server) GetProducts() []Product {
	s.initWawit.Wait()
	return s.getProductListFromLocalCache()
}

func (s *Server) getProductListFromLocalCache() []Product {
	s.productMapMutex.Lock()
	defer s.productMapMutex.Unlock()

	var result []Product
	for _, v := range s.productMap {
		result = append(result, *v)
	}
	return result
}

func (s *Server) GetProduct(number string) *Product {
	s.initWawit.Wait()

	s.productMapMutex.Lock()
	defer s.productMapMutex.Unlock()

	return s.productMap[number]
}

func (s *Server) IsProductExists(number string) bool {
	return s.GetProduct(number) != nil
}

func (s *Server) IsExists(number string) bool {
	s.productMapMutex.Lock()
	defer s.productMapMutex.Unlock()
	// 从map中判断
	_, ok := s.productMap[number]
	return ok
}
