
## 自动加载 sites/ 下的第一个站点
go run ./cmd/server/

## 指定站点配置
go run ./cmd/server/ -config local-test/hurricane-techs/localhost/config.toml

## 切换到 civic estate 主题
go run ./cmd/server/ -config local-test/civic-estate/localhost/config.toml

## 切换到 FloraFi 主题
go run ./cmd/server/ -config local-test/florafi/localhost/config.toml

## 切换到 Axis Form 主题
go run ./cmd/server/ -config local-test/axis-form/localhost/config.toml

## 切换到 atelier-slate 主题
go run ./cmd/server/ -config local-test/atelier-slate/localhost/config.toml

## 切换到 terra-trail 主题
go run ./cmd/server/ -config local-test/terra-trail/localhost/config.toml
