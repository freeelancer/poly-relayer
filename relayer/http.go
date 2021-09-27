/*
 * Copyright (C) 2021 The poly network Authors
 * This file is part of The poly network library.
 *
 * The  poly network  is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The  poly network  is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 * You should have received a copy of the GNU Lesser General Public License
 * along with The poly network .  If not, see <http://www.gnu.org/licenses/>.
 */

package relayer

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/beego/beego/v2/server/web"
	"github.com/urfave/cli/v2"

	"github.com/polynetwork/bridge-common/base"
	"github.com/polynetwork/bridge-common/log"
	"github.com/polynetwork/bridge-common/metrics"
	"github.com/polynetwork/bridge-common/util"
	"github.com/polynetwork/poly-relayer/bus"
	"github.com/polynetwork/poly-relayer/config"
	"github.com/polynetwork/poly-relayer/msg"
)

var (
	_PATCHER *bus.RedisTxBus
)

func Http(ctx *cli.Context) (err error) {
	metrics.Init("relayer")
	// Insert web config
	port := ctx.Int("port")
	host := ctx.String("host")
	if port == 0 {
		port = config.CONFIG.Port
	}
	if host == "" {
		host = config.CONFIG.Host
	}

	// Init patcher
	_PATCHER = bus.NewRedisPatchTxBus(bus.New(config.CONFIG.Bus.Redis), 0)

	web.AddNamespace(
		web.NewNamespace("/api",
			web.NSNamespace("/",
				web.NSRouter("patch", &PatchController{}, "get:Patch"),
			),
		),
	)

	go recordMetrics()

	web.BConfig.Listen.HTTPAddr = host
	web.BConfig.Listen.HTTPPort = port
	web.BConfig.RunMode = "prod"
	web.BConfig.AppName = "relayer"
	web.Run()
	return
}

func recordMetrics() {
	h := NewStatusHandler(config.CONFIG.Bus.Redis)
	timer := time.NewTicker(2 * time.Second)
	for range timer.C {
		for _, chain := range base.CHAINS {
			name := base.GetChainName(chain)
			name = strings.ReplaceAll(name, "(", "")
			name = strings.ReplaceAll(name, ")", "")
			latest, _ := h.Height(chain, bus.KEY_HEIGHT_CHAIN)
			header, _ := h.Height(chain, bus.KEY_HEIGHT_HEADER)
			tx, _ := h.Height(chain, bus.KEY_HEIGHT_TX)
			metrics.Record(header, "height.header_sync.%s", name)
			metrics.Record(tx, "height.tx_sync.%s", name)
			metrics.Record(latest, "height.node.%s", name)
			if latest > 0 {
				metrics.Record(latest-header, "height_diff.header_sync.%s", name)
				metrics.Record(latest-tx, "height_diff.tx_sync.%s", name)
			}
			qSrc, _ := h.Len(chain, msg.SRC)
			qPoly, _ := h.Len(chain, msg.POLY)
			metrics.Record(qSrc, "queue_size.src.%s", name)
			metrics.Record(qPoly, "queue_size.poly.%s", name)
		}
	}
}

type PatchController struct {
	web.Controller
}

func (c *PatchController) Patch() {
	height, _ := strconv.Atoi(c.Ctx.Input.Query("height"))
	chain, _ := strconv.Atoi(c.Ctx.Input.Query("chain"))
	hash := c.Ctx.Input.Query("hash")
	tx := &msg.Tx{}
	if chain == 0 {
		tx.PolyHeight = uint32(height)
		tx.PolyHash = hash
	} else {
		tx.SrcHash = hash
		tx.SrcHeight = uint64(height)
	}
	log.Info("Patching tx", "body", tx.Encode())
	err := _PATCHER.PushToChain(context.Background(), tx)
	if err != nil {
		c.Data["json"] = err.Error()
		c.Ctx.ResponseWriter.WriteHeader(400)
	} else {
		c.Data["json"] = tx
	}
	c.ServeJSON()
}

func Patch(ctx *cli.Context) (err error) {
	height := uint64(ctx.Int("height"))
	chain := uint64(ctx.Int("chain"))
	hash := ctx.String("hash")
	tx := &msg.Tx{}
	if chain == 0 {
		tx.PolyHeight = uint32(height)
		tx.PolyHash = hash
	} else {
		tx.SrcHash = hash
		tx.SrcHeight = height
	}
	err = bus.NewRedisPatchTxBus(bus.New(config.CONFIG.Bus.Redis), 0).PushToChain(context.Background(), tx)
	if err != nil {
		log.Error("Patch tx failed", "err", err)
		fmt.Println(util.Verbose(tx))
	}
	return
}