/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2019 WireGuard LLC. All Rights Reserved.
 */

package conf

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

func maybeMigrateConfiguration(c string) {
	if disableAutoMigration {
		return
	}
	oldRoot, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return
	}
	oldC := filepath.Join(oldRoot, "WireGuard", "Configurations")
	files, err := ioutil.ReadDir(oldC)
	if err != nil {
		return
	}
	migratedConfigs := make(map[string]string)
	for i := range files {
		if files[i].IsDir() {
			continue
		}
		fileName := files[i].Name()
		newPath := filepath.Join(c, fileName)
		newFile, err := os.OpenFile(newPath, os.O_EXCL|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			continue
		}
		oldPath := filepath.Join(oldC, fileName)
		oldConfig, err := ioutil.ReadFile(oldPath)
		if err != nil {
			newFile.Close()
			os.Remove(newPath)
			continue
		}
		_, err = newFile.Write(oldConfig)
		if err != nil {
			newFile.Close()
			os.Remove(newPath)
			continue
		}
		newFile.Close()
		oldPath16, err := windows.UTF16PtrFromString(oldPath)
		if err == nil {
			windows.MoveFileEx(oldPath16, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
		}
		migratedConfigs[strings.ToLower(oldPath)] = newPath
		log.Printf("Migrated configuration from ‘%s’ to ‘%s’", oldPath, newPath)
	}
	oldC16, err := windows.UTF16PtrFromString(oldC)
	if err == nil {
		windows.MoveFileEx(oldC16, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	}
	oldLog16, err := windows.UTF16PtrFromString(filepath.Join(oldRoot, "WireGuard", "log.bin"))
	if err == nil {
		windows.MoveFileEx(oldLog16, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	}
	oldRoot16, err := windows.UTF16PtrFromString(filepath.Join(oldRoot, "WireGuard"))
	if err == nil {
		windows.MoveFileEx(oldRoot16, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	}
	if len(migratedConfigs) == 0 {
		return
	}
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()
	services, err := m.ListServices()
	if err != nil {
		return
	}
	matcher, err := regexp.Compile(" /tunnelservice \"?([^\"]+)\"?$")
	if err != nil {
		return
	}
	for _, svcName := range services {
		if !strings.HasPrefix(svcName, "WireGuardTunnel$") {
			continue
		}
		svc, err := m.OpenService(svcName)
		if err != nil {
			continue
		}
		config, err := svc.Config()
		if err != nil {
			continue
		}
		matches := matcher.FindStringSubmatchIndex(config.BinaryPathName)
		if len(matches) != 4 {
			svc.Close()
			continue
		}
		newName, found := migratedConfigs[strings.ToLower(config.BinaryPathName[matches[2]:])]
		if !found {
			svc.Close()
			continue
		}
		config.BinaryPathName = config.BinaryPathName[:matches[0]] + fmt.Sprintf(" /tunnelservice \"%s\"", newName)
		err = svc.UpdateConfig(config)
		svc.Close()
		if err != nil {
			continue
		}
		log.Printf("Migrated service command line arguments for ‘%s’", svcName)
	}
}
