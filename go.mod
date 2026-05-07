module github.com/gmcorenet/gmcore

go 1.23

require (
	github.com/gmcorenet/sdk-gmcore-transport v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/gmcorenet/sdk-gmcore-config v1.0.0 // indirect
	github.com/gmcorenet/sdk-gmcore-error v1.0.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	gorm.io/gorm v1.25.10 // indirect
)

replace (
	github.com/gmcorenet/sdk-gmcore-config => ../../packages/sdks/gmcore-config
	github.com/gmcorenet/sdk-gmcore-error => ../../packages/sdks/gmcore-error
	github.com/gmcorenet/sdk-gmcore-maker => ../../packages/sdks/gmcore-maker
	github.com/gmcorenet/sdk-gmcore-transport => ../../packages/sdks/gmcore-transport
)
