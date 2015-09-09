package redis

import (
	"fmt"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/golang/glog"

	"github.com/cesanta/docker_auth/auth_server/authn/tokenstore"
	"github.com/cesanta/docker_auth/auth_server/authn/tokenstore/factory"
)

var StoreName = "redis"

func init() {
	factory.Register(StoreName, &redisFactory{
		config: &redisConfig{},
	})
}

// redisFactory implements factory.TokenStoreFactory for redis
type redisFactory struct {
	config *redisConfig
}

func (factory *redisFactory) UnmarshalYAML(unmarshal func(interface{}) error) error {
	return unmarshal(factory.config)
}

type redisConfig struct {
	// Redis instance
	Addr     string `yaml:"addr"`
	Password string `yaml:"password,omitempty"`
	// DB to connect to on redis instance
	DB int `yaml:"db,omitempty"`
	// Timeout configuration
	Timeout struct {
		Dial  time.Duration `yaml:"dial,omitempty"`  // connect timeout
		Read  time.Duration `yaml:"read,omitempty"`  // read timeout
		Write time.Duration `yaml:"write,omitempty"` // write timeout
		Idle  time.Duration `yaml:"idle,omitempty"`  // inactive connection timeout
	} `yaml:"timeout,omitempty"`
	// Pool configuration
	Pool struct {
		MaxIdle   int `yaml:"maxidle,omitempty"`   // max idle connections
		MaxActive int `yaml:"maxactive,omitempty"` // max open connections before blocking
	} `yaml:"pool,omitempty"`
}

func (c *redisConfig) Validate() error {
	if c.Addr == "" {
		return fmt.Errorf("missing address")
	}
	return nil
}

func (factory *redisFactory) Create() (tokenstore.TokenStore, error) {
	if factory.config == nil {
		return nil, fmt.Errorf("%s: nil configuration", StoreName)
	}
	if err := factory.config.Validate(); err != nil {
		return nil, fmt.Errorf("%s: invalid configuration: %s", StoreName, err)
	}
	s := &store{
		Config: factory.config,
	}
	s.Pool = &redis.Pool{
		MaxIdle:     s.Config.Pool.MaxIdle,
		MaxActive:   s.Config.Pool.MaxActive,
		IdleTimeout: s.Config.Timeout.Idle,
		Wait:        false,
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
		Dial: func() (redis.Conn, error) {
			c, err := redis.DialTimeout("tcp",
				s.Config.Addr,
				s.Config.Timeout.Dial,
				s.Config.Timeout.Read,
				s.Config.Timeout.Write)
			if err != nil {
				glog.Errorf("error connecting to redis instance %s: %v", s.Config.Addr, err)
				return nil, err
			}
			if s.Config.Password != "" {
				if _, err := c.Do("AUTH", s.Config.Password); err != nil {
					c.Close()
					return nil, err
				}
			}
			if s.Config.DB != 0 {
				if _, err := c.Do("SELECT", s.Config.DB); err != nil {
					c.Close()
					return nil, err
				}
			}
			return c, nil
		},
	}

	// Check connectivity
	if err := s.Ping(); err != nil {
		return nil, err
	}

	return s, nil
}

type store struct {
	Config *redisConfig
	Pool   *redis.Pool
}

func (s *store) String() string {
	return fmt.Sprintf("%s: %s", StoreName, s.Config.Addr)
}

func (s *store) Ping() error {
	conn := s.Pool.Get()
	defer conn.Close()
	_, err := conn.Do("PING")
	return err
}

func (s *store) Close() error {
	return s.Pool.Close()
}

func (s *store) Get(key []byte) ([]byte, error) {
	conn := s.Pool.Get()
	defer conn.Close()
	value, err := redis.Bytes(conn.Do("GET", key))
	if err == redis.ErrNil {
		err = tokenstore.ErrNotFound
	}
	return value, err
}

func (s *store) Put(key, value []byte) error {
	conn := s.Pool.Get()
	defer conn.Close()
	_, err := conn.Do("SET", key, value)
	return err
}

func (s *store) Delete(key []byte) error {
	conn := s.Pool.Get()
	defer conn.Close()
	_, err := conn.Do("DEL", key)
	return err
}
