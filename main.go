package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/jaxxstorm/flexvolume"
	"github.com/kolyshkin/goploop-cli"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/virtuozzo/ploop-flexvol/vstorage"
)

func main() {
	app := cli.NewApp()
	app.Name = "ploop flexvolume"
	app.Usage = "Mount ploop volumes in kubernetes using the flexvolume driver"
	app.Commands = flexvolume.Commands(Ploop{})
	app.CommandNotFound = flexvolume.CommandNotFound
	app.Authors = []cli.Author{
		cli.Author{
			Name: "Lee Briggs",
		},
		cli.Author{
			Name: "Virtuozzo",
		},
	}
	app.Version = "0.2a"

	flexvolume.SetRespFile(os.NewFile((uintptr)(3), "RespFile"))

	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.DebugLevel)

	logrus.Debugf("New request: %v", os.Args)
	app.Run(os.Args)
}

type Ploop struct{}

const WorkingDir = "/var/run/ploop-flexvol/"

func (p Ploop) Init() (*flexvolume.Response, error) {
	return &flexvolume.Response{
		Status:  flexvolume.StatusSuccess,
		Message: "Ploop is available",
	}, nil
}

func (p Ploop) path(options map[string]string) string {
	path := "/"
	if options["volumePath"] != "" {
		path += options["volumePath"] + "/"
	}
	path += options["volumeId"]
	return path
}

func (p Ploop) GetVolumeName(options map[string]string) (*flexvolume.Response, error) {
	if options["volumeId"] == "" {
		return nil, fmt.Errorf("Must specify a volume id")
	}

	return &flexvolume.Response{
		Status:     flexvolume.StatusSuccess,
		VolumeName: options["volumeId"],
	}, nil
}

func prepareVstorage(clusterName, clusterPasswd string, mount string) error {
	mounted, _ := vstorage.IsVstorage(mount)
	if mounted {
		return nil
	}

	// not mounted in proper place, prepare mount place and check other
	// mounts
	if err := os.MkdirAll(mount, 0755); err != nil {
		return err
	}

	v := vstorage.Vstorage{clusterName}
	p, _ := v.Mountpoint()
	if p != "" {
		return syscall.Mount(p, mount, "", syscall.MS_BIND, "")
	}

	if clusterPasswd == "" {
		return errors.New("Please provide vstorage credentials")
	}

	if err := v.Auth(clusterPasswd); err != nil {
		return err
	}
	if err := v.Mount(mount); err != nil {
		return err
	}

	return nil
}

func (p Ploop) Mount(target string, options map[string]string) (*flexvolume.Response, error) {
	// make the target directory we're going to mount to
	err := os.MkdirAll(target, 0755)
	if err != nil {
		return nil, err
	}

	path := p.path(options)

	if options["kubernetes.io/secret/clusterName"] != "" {
		_cluster, err := base64.StdEncoding.DecodeString(options["kubernetes.io/secret/clusterName"])
		if err != nil {
			return nil, fmt.Errorf("Unable to decode a cluster name: %v", err.Error())
		}
		cluster := string(_cluster)

		_passwd, err := base64.StdEncoding.DecodeString(options["kubernetes.io/secret/clusterPassword"])
		if err != nil {
			return nil, fmt.Errorf("Unable to decode a cluster password: %v", err.Error())
		}
		passwd := string(_passwd)

		mount := WorkingDir + cluster
		if err := prepareVstorage(cluster, passwd, mount); err != nil {
			return nil, err
		}
		path = mount + path
	}
	// open the disk descriptor first
	volume, err := ploop.Open(path + "/" + "DiskDescriptor.xml")
	if err != nil {
		return nil, err
	}
	defer volume.Close()

	if m, _ := volume.IsMounted(); !m {
		// If it's mounted, let's mount it!

		readonly := false
		if options["kubernetes.io/readwrite"] == "ro" {
			readonly = true
		}

		mp := ploop.MountParam{Target: target, Readonly: readonly}

		_, err := volume.Mount(&mp)
		if err != nil {
			return nil, err
		}

		return &flexvolume.Response{
			Status:  flexvolume.StatusSuccess,
			Message: "Successfully mounted the ploop volume",
		}, nil
	} else {

		return &flexvolume.Response{
			Status:  flexvolume.StatusSuccess,
			Message: "Ploop volume already mounted",
		}, nil

	}
}

func (p Ploop) Unmount(mount string) (*flexvolume.Response, error) {
	if err := ploop.UmountByMount(mount); err != nil {
		return nil, err
	}

	return &flexvolume.Response{
		Status:  flexvolume.StatusSuccess,
		Message: "Successfully unmounted the ploop volume",
	}, nil
}
