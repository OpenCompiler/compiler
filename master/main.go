package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-units"
	"github.com/gin-gonic/gin"
	"golang.org/x/net/context"
	"gopkg.in/yaml.v2"
)

type Build struct {
	Code     string `form:"code"`
	Language string `form:"language"`
}

type Run struct {
	Code     string `form:"code"`
	Language string `form:"language"`
	Stdin    string `form:"stdin"`
}

type Language struct {
	Name        string   `yaml:"name"`
	DockerImage string   `yaml:"docker_image"`
	BuildCmd    []string `yaml:"build_cmd"`
	RunCmd      []string `yaml:"run_cmd"`
	CodeFile    string   `yaml:"code_file"`
}

type Languages struct {
	Language map[string]Language `yaml:"language"`
}

func main() {
	ctx := context.Background()

	// Read languges setttings
	buf, err := ioutil.ReadFile("./languages.yaml")
	if err != nil {
		fmt.Println(err.Error())
	}
	var lang Languages
	err = yaml.Unmarshal(buf, &lang)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%#v\n", lang)

	// Create docker client
	cli, err := client.NewEnvClient()
	if err != nil {
		log.Fatal("Cannot create docker client")
	}
	options := types.ContainerListOptions{All: true}

	for {
		ver, err := cli.ServerVersion(ctx)
		if err == nil {
			fmt.Println(ver.Version)
			break
		}
		time.Sleep(1 * time.Second)
	}
	// Pull using images
	timeout := time.Duration(1 * time.Second)
	if _, err := net.DialTimeout("tcp", "hub.docker.com:80", timeout); err != nil {
		log.Println("Site unreachable, error: ", err)
	} else {
		for _, v := range lang.Language {
			res, err := cli.ImagePull(ctx, v.DockerImage, types.ImagePullOptions{})
			if err != nil {
				log.Fatal(err)
			}
			io.Copy(os.Stdout, res)
		}
	}

	// Start routing
	r := gin.Default()
	r.GET("/", func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.String(http.StatusOK, "pong")
	})
	r.GET("/language", func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.JSON(http.StatusOK, gin.H{
			"languages": lang.Language,
		})
	})
	r.GET("/node", func(c *gin.Context) {
		containers, err := cli.ContainerList(ctx, options)
		if err != nil {
			log.Print(err)
			c.String(http.StatusInternalServerError, err.Error())
		}
		c.JSON(http.StatusOK, gin.H{
			"containers": containers,
		})
	})
	r.POST("/run", gin.WrapF(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		var query Run
		bufbody := new(bytes.Buffer)
		bufbody.ReadFrom(r.Body)
		body := bufbody.Bytes()
		if err := json.Unmarshal(body, &query); err == nil {
			// Make hash
			h := md5.New()
			io.WriteString(h, query.Language)
			io.WriteString(h, query.Code)
			runningHash := hex.EncodeToString(h.Sum(nil))
			// Check exist of source code and builded image
			_, err = os.Stat("/tmp/compiler/" + runningHash + "/" + lang.Language[query.Language].CodeFile)
			if err != nil {
				// Check this language requires build command
				if len(lang.Language[query.Language].BuildCmd) == 0 {
					// Save code
					if err := os.MkdirAll("/tmp/compiler/"+runningHash, 0755); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						w.Write([]byte(err.Error()))
						fmt.Println(err.Error())
						return
					}
					fp, err := os.OpenFile("/tmp/compiler/"+runningHash+"/"+lang.Language[query.Language].CodeFile, os.O_WRONLY|os.O_CREATE, 0644)
					if err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						w.Write([]byte(err.Error()))
						fmt.Println(err.Error())
						return
					}
					defer fp.Close()
					writer := bufio.NewWriter(fp)
					_, err = writer.WriteString(query.Code)
					if err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						w.Write([]byte(err.Error()))
						fmt.Println(err.Error())
						return
					}
					writer.Flush()
				} else {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte("Should /build before /run"))
				}
			}

			// Create container
			// TODO: Limit container spec
			resp, err := cli.ContainerCreate(ctx, &container.Config{
				Image:           lang.Language[query.Language].DockerImage,
				WorkingDir:      "/workspace",
				Cmd:             lang.Language[query.Language].RunCmd,
				NetworkDisabled: true,
				AttachStdin:     true,
				AttachStdout:    true,
				AttachStderr:    true,
				OpenStdin:       true,
				StdinOnce:       true,
				Tty:             false,
			}, &container.HostConfig{
				Mounts: []mount.Mount{
					mount.Mount{
						Type:   mount.TypeBind,
						Source: "/tmp/compiler/" + runningHash,
						Target: "/workspace",
					},
				},
				Resources: container.Resources{
					Memory:    512 * 1024 * 1024,
					PidsLimit: 64,
					Ulimits: []*units.Ulimit{
						{
							Name: "nproc",
							Hard: 64,
							Soft: 64,
						},
						{
							Name: "fsize",
							Hard: 10000000,
							Soft: 10000000,
						},
						{
							Name: "core",
							Hard: -1,
							Soft: -1,
						},
					},
				},
				AutoRemove: true,
			}, nil, "")
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(err.Error()))
				fmt.Println(err.Error())
				return
			}

			// Attach container
			stdin, err := cli.ContainerAttach(ctx, resp.ID, types.ContainerAttachOptions{
				Stream: true,
				Stdin:  true,
			})
			defer stdin.Close()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(err.Error()))
				fmt.Println(err.Error())
				return
			}

			containerOutput, err := cli.ContainerAttach(ctx, resp.ID, types.ContainerAttachOptions{
				Stream: true,
				Stdout: true,
				Stderr: true,
			})
			defer containerOutput.Close()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(err.Error()))
				fmt.Println(err.Error())
				return
			}

			// Start container
			err = cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{})
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(err.Error()))
				fmt.Println(err.Error())
				return
			}

			// Put to Stdin
			stdin.Conn.Write([]byte(query.Stdin))
			stdin.CloseWrite()

			// Flow log of Stdout
			_, err = stdcopy.StdCopy(w, w, containerOutput.Reader)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(err.Error()))
				fmt.Println(err.Error())
				return
			}
		} else {
			//c.String(http.StatusBadRequest, err.Error())
		}
	}))
	r.Run()
}
