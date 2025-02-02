package main

import (
  "bytes"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Dreamacro/clash/adapter"
	"github.com/Dreamacro/clash/adapter/provider"
	C "github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/log"
	"gopkg.in/yaml.v3"
)

var (
	livenessObject     = flag.String("l", "https://speed.cloudflare.com/__down?bytes=%d", "liveness object, support http(s) url, support payload too")
	configPathConfig   = flag.String("c", "", "configuration file path, also support http(s) url")
	filterRegexConfig  = flag.String("f", ".*", "filter proxies by name, use regexp")
	downloadSizeConfig = flag.Int("size", 1024*1024*100, "download size for testing proxies")
	timeoutConfig      = flag.Duration("timeout", time.Second*5, "timeout for testing proxies")
	sortField          = flag.String("sort", "b", "sort field for testing proxies, b for bandwidth, t for TTFB")
	output             = flag.String("output", "", "output result to csv/yaml file")
	concurrent         = flag.Int("concurrent", 4, "download concurrent size")
  outfile            = flag.String("outfile", "result", "outfile name")
  band_thred         = flag.Float64("widthred", -0.1, "less than this value, don't output to outfile")
)

type CProxy struct {
	C.Proxy
	SecretConfig any
}

type Result struct {
	Name      string
	Bandwidth float64
	TTFB      time.Duration
}

var (
	red   = "\033[31m"
	green = "\033[32m"
)

type RawConfig struct {
	Providers map[string]map[string]any `yaml:"proxy-providers"`
	Proxies   []map[string]any          `yaml:"proxies"`
}

func main() {
	flag.Parse()

	C.UA = "clash.meta"

	if *configPathConfig == "" {
		log.Fatalln("Please specify the configuration file")
	}

	var allProxies = make(map[string]CProxy)
	for _, configPath := range strings.Split(*configPathConfig, ",") {
		var body []byte
		var err error
		if strings.HasPrefix(configPath, "http") {
			var resp *http.Response
			resp, err = http.Get(configPath)
			if err != nil {
				log.Warnln("failed to fetch config: %s", err)
				continue
			}
			body, err = io.ReadAll(resp.Body)
		} else {
			body, err = os.ReadFile(configPath)
		}
		if err != nil {
			log.Warnln("failed to read config: %s", err)
			continue
		}

		lps, err := loadProxies(body)
		if err != nil {
			log.Fatalln("Failed to convert : %s", err)
		}

		for k, p := range lps {
			if _, ok := allProxies[k]; !ok {
				allProxies[k] = p
			}
		}
	}

	filteredProxies := filterProxies(*filterRegexConfig, allProxies)
	results := make([]Result, 0, len(filteredProxies))

	format := "%s%-42s\t%-12s\t%-12s\033[0m\n"

	fmt.Printf(format, "", "节点", "带宽", "延迟")
	for _, name := range filteredProxies {
		proxy := allProxies[name]
		switch proxy.Type() {
		case C.Shadowsocks, C.ShadowsocksR, C.Snell, C.Socks5, C.Http, C.Vmess, C.Vless, C.Trojan, C.Hysteria, C.Hysteria2, C.WireGuard, C.Tuic:
			result := TestProxyConcurrent(name, proxy, *downloadSizeConfig, *timeoutConfig, *concurrent)
			result.Printf(format)
			results = append(results, *result)
		case C.Direct, C.Reject, C.Relay, C.Selector, C.Fallback, C.URLTest, C.LoadBalance:
			continue
		default:
			log.Fatalln("Unsupported proxy type: %s", proxy.Type())
		}
	}

	if *sortField != "" {
		switch *sortField {
		case "b", "bandwidth":
			sort.Slice(results, func(i, j int) bool {
				return results[i].Bandwidth > results[j].Bandwidth
			})
			fmt.Println("\n\n===结果按照带宽排序===")
		case "t", "ttfb":
			sort.Slice(results, func(i, j int) bool {
				return results[i].TTFB < results[j].TTFB
			})
			fmt.Println("\n\n===结果按照延迟排序===")
		default:
			log.Fatalln("Unsupported sort field: %s", *sortField)
		}
		fmt.Printf(format, "", "节点", "带宽", "延迟")
		for _, result := range results {
			result.Printf(format)
		}
	}

	if strings.EqualFold(*output, "yaml") {
		if err := writeNodeConfigurationToYAML(*outfile+".yaml", results, allProxies, *band_thred); err != nil {
			log.Fatalln("Failed to write yaml: %s", err)
		}
	} else if strings.EqualFold(*output, "csv") {
		if err := writeToCSV(*outfile+".csv", results); err != nil {
			log.Fatalln("Failed to write csv: %s", err)
		}
	}
}

func filterProxies(filter string, proxies map[string]CProxy) []string {
	filterRegexp := regexp.MustCompile(filter)
	filteredProxies := make([]string, 0, len(proxies))
	for name := range proxies {
		if filterRegexp.MatchString(name) {
			filteredProxies = append(filteredProxies, name)
		}
	}
	sort.Strings(filteredProxies)
	return filteredProxies
}

func loadProxies(buf []byte) (map[string]CProxy, error) {
	rawCfg := &RawConfig{
		Proxies: []map[string]any{},
	}

  quot := []byte("&quot")
  quot_full := []byte("&quot;")
  star_mark := []byte("*")
  q_mark := []byte("?")
  replace_str := []byte("")
  n := -1

  tmp_buf := bytes.Replace(buf, quot_full, replace_str, n)
  s_buf := bytes.Replace(tmp_buf, quot, replace_str, n)
  st_buf :=  bytes.Replace(s_buf, star_mark, replace_str, n)
  tmp_buf = bytes.Replace(st_buf, q_mark, replace_str, n)
//  for j := 0; j < len(buf); {
//    if bytes.HasPrefix(buf[j:], quot) {
//      bytes.Replace(buf[j:], old, "     ", n)
//      //copy(result[i:], new)
//      i += len(new)
//      j += len(old)
//    } else {
//      i++
//      j++
//    }
//  }
//  obfs_cipher := []byte("aes-128-gcm")
//  if bytes.Contains(tmp_buf, obfs_cipher) {
//		return nil, fmt.Errorf("proxy obfs not support cipher %s ", obfs_cipher)
//  }
	if err := yaml.Unmarshal(tmp_buf, rawCfg); err != nil {
    log.Warnln("Self_:Unmarshal rawCfg , err.")
		return nil, err
	}

	proxies := make(map[string]CProxy)
	proxiesConfig := rawCfg.Proxies
	providersConfig := rawCfg.Providers

  cipher_str := "aes-128-gcm" //"chacha20-ietf-poly1305"
  type_trojan := "trojan" // []byte("trojan")
  type_vmess := "vmess" //[]byte("vmess")

	for i, config := range proxiesConfig {
    type_val, ok := config["type"]
    if !ok {
      log.Warnln("proxy %d node type is error.", i)
      continue
    }
    //if !bytes.Equal(type_trojan, bytes.ToLower([]byte(type_val)))
    stype, yok := type_val.(string)
    if !yok {
      log.Warnln("proxy type uuid is illeage, proxy: %d", i)
      continue
    }
    if strings.EqualFold(type_trojan, stype) != true {
      val, ok := config["cipher"]
      if !ok {
        log.Warnln("Not trojan proxy, and cipher error, proxy: %d", i)
        continue
      }
      sval, yok := val.(string)
      if yok {
        if strings.Contains(sval, cipher_str) {
          fmt.Println("obfs cipher", cipher_str)
          continue
        }
      } else {
        log.Warnln("%s to string error, proxy: %d", val, i)
      }
    }

    //if bytes.Equal(type_vmess, bytes.ToLower([]byte(type_val)))
    if strings.EqualFold(type_vmess, stype) {
      uuid_val, ok := config["uuid"]
      if ok {
        suuid, yok := uuid_val.(string)
        if !yok {
          log.Warnln("trojan type uuid is illeage, proxy: %d", i)
          continue
        }
        strCount := strings.Count(suuid, "")
        if strCount != 37 {
          log.Warnln("trojan type uuid len:%d isn't equal 37, proxy: %d, %s", strCount, i, suuid)
          continue
        }
      } else {
        log.Warnln("trojan type hasn't uuid, proxy: %d", i)
        continue
      }
    }
		proxy, err := adapter.ParseProxy(config)
		if err != nil {
      log.Warnln("proxy %d: %w", i, err)
      continue
			return nil, fmt.Errorf("proxy %d: %w", i, err)
		}

		if _, exist := proxies[proxy.Name()]; exist {
      log.Warnln("proxy %s is the duplicate name", proxy.Name())
      continue
			return nil, fmt.Errorf("proxy %s is the duplicate name", proxy.Name())
		}
    // proxy_json, err := proxy.MarshalJSON()
    // print( proxy_json);
		proxies[proxy.Name()] = CProxy{Proxy: proxy, SecretConfig: config}
	}
  ii := 0
	for name, config := range providersConfig {
    ii++
    log.Warnln("Self_Provider:%d name: %s.", ii, name)
		if name == provider.ReservedName {
			return nil, fmt.Errorf("can not defined a provider called `%s`", provider.ReservedName)
		}

    type_val, ok := config["type"]
    if !ok {
      log.Warnln("proxy %d node type is error.", ii)
      continue
    }

    stype, yok := type_val.(string)
    if !yok {
      log.Warnln("proxy type uuid is illeage, proxy: %d", ii)
      continue
    }
    //if !bytes.Equal(type_trojan, bytes.ToLower([]byte(type_val))) {
    if strings.EqualFold(type_trojan, stype) != true {
      val, ok := config["cipher"]
      if !ok {
        log.Warnln("Not trojan proxy, and cipher error, proxy: %d", ii)
        continue
      }
      sval, yok := val.(string)
      if yok {
        if strings.Contains(sval, cipher_str) {
          fmt.Println("obfs cipher", cipher_str)
          continue
        }
      } else {
        log.Warnln("%s to string error, proxy: %d", val, ii)
      }
    }

    //if bytes.Equal(type_vmess, bytes.ToLower([]byte(type_val))) {
    if strings.EqualFold(type_vmess, stype) {
      uuid_val, ok := config["uuid"]
      if ok {
        suuid, yok := uuid_val.(string)
        if !yok {
          log.Warnln("trojan type uuid is illeage, proxy: %d", ii)
          continue
        }
        strCount := strings.Count(suuid, "")
        if strCount != 37 {
          log.Warnln("trojan type uuid len:%d isn't equal 37, proxy: %d, %s", strCount, ii, suuid)
          continue
        }
      } else {
        log.Warnln("trojan type hasn't uuid, proxy: %d", ii)
        continue
      }
    }
		pd, err := provider.ParseProxyProvider(name, config)
		if err != nil {
			return nil, fmt.Errorf("parse proxy provider %s error: %w", name, err)
		}
		if err := pd.Initial(); err != nil {
			return nil, fmt.Errorf("initial proxy provider %s error: %w", pd.Name(), err)
		}
		for _, proxy := range pd.Proxies() {
			proxies[fmt.Sprintf("[%s] %s", name, proxy.Name())] = CProxy{Proxy: proxy}
		}
	}
	return proxies, nil
}

func (r *Result) Printf(format string) {
	color := ""
	if r.Bandwidth < 1024*1024 {
		color = red
	} else if r.Bandwidth > 1024*1024*10 {
		color = green
	}
	fmt.Printf(format, color, formatName(r.Name), formatBandwidth(r.Bandwidth), formatMilliseconds(r.TTFB))
}

func TestProxyConcurrent(name string, proxy C.Proxy, downloadSize int, timeout time.Duration, concurrentCount int) *Result {
	if concurrentCount <= 0 {
		concurrentCount = 1
	}

	chunkSize := downloadSize / concurrentCount
	totalTTFB := int64(0)
	downloaded := int64(0)

	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func(i int) {
			result, w := TestProxy(name, proxy, chunkSize, timeout)
			if w != 0 {
				atomic.AddInt64(&downloaded, w)
				atomic.AddInt64(&totalTTFB, int64(result.TTFB))
			}
			wg.Done()
		}(i)
	}
	wg.Wait()
	downloadTime := time.Since(start)

	result := &Result{
		Name:      name,
		Bandwidth: float64(downloaded) / downloadTime.Seconds(),
		TTFB:      time.Duration(totalTTFB / int64(concurrentCount)),
	}

	return result
}

func TestProxy(name string, proxy C.Proxy, downloadSize int, timeout time.Duration) (*Result, int64) {
	client := http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				var u16Port uint16
				if port, err := strconv.ParseUint(port, 10, 16); err == nil {
					u16Port = uint16(port)
				}
				return proxy.DialContext(ctx, &C.Metadata{
					Host:    host,
					DstPort: u16Port,
				})
			},
		},
	}

	start := time.Now()
	resp, err := client.Get(fmt.Sprintf(*livenessObject, downloadSize))
	if err != nil {
		return &Result{name, -1, -1}, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode-http.StatusOK > 100 {
		return &Result{name, -1, -1}, 0
	}
	ttfb := time.Since(start)

	written, _ := io.Copy(io.Discard, resp.Body)
	if written == 0 {
		return &Result{name, -1, -1}, 0
	}
	downloadTime := time.Since(start) - ttfb
	bandwidth := float64(written) / downloadTime.Seconds()

	return &Result{name, bandwidth, ttfb}, written
}

var (
	emojiRegex = regexp.MustCompile(`[\x{1F600}-\x{1F64F}\x{1F300}-\x{1F5FF}\x{1F680}-\x{1F6FF}\x{2600}-\x{26FF}\x{1F1E0}-\x{1F1FF}]`)
	spaceRegex = regexp.MustCompile(`\s{2,}`)
)

func formatName(name string) string {
	noEmoji := emojiRegex.ReplaceAllString(name, "")
	mergedSpaces := spaceRegex.ReplaceAllString(noEmoji, " ")
	return strings.TrimSpace(mergedSpaces)
}

func formatBandwidth(v float64) string {
	if v <= 0 {
		return "N/A"
	}
	if v < 1024 {
		return fmt.Sprintf("%.02fB/s", v)
	}
	v /= 1024
	if v < 1024 {
		return fmt.Sprintf("%.02fKB/s", v)
	}
	v /= 1024
	if v < 1024 {
		return fmt.Sprintf("%.02fMB/s", v)
	}
	v /= 1024
	if v < 1024 {
		return fmt.Sprintf("%.02fGB/s", v)
	}
	v /= 1024
	return fmt.Sprintf("%.02fTB/s", v)
}

func formatMilliseconds(v time.Duration) string {
	if v <= 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.02fms", float64(v.Milliseconds()))
}

func writeNodeConfigurationToYAML(filePath string, results []Result, proxies map[string]CProxy, band_thred float64) error {
	fp, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer fp.Close()

	var sortedProxies []any
	for _, result := range results {
		if v, ok := proxies[result.Name]; ok {
      if result.Bandwidth <= band_thred {
        continue
      }
			sortedProxies = append(sortedProxies, v.SecretConfig)
		}
	}

	bytes, err := yaml.Marshal(sortedProxies)
	if err != nil {
		return err
	}

	_, err = fp.Write(bytes)
	return err
}

func writeToCSV(filePath string, results []Result) error {
	csvFile, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer csvFile.Close()

	// 写入 UTF-8 BOM 头
	csvFile.WriteString("\xEF\xBB\xBF")

	csvWriter := csv.NewWriter(csvFile)
	err = csvWriter.Write([]string{"节点", "带宽 (MB/s)", "延迟 (ms)"})
	if err != nil {
		return err
	}
	for _, result := range results {
		line := []string{
			result.Name,
			fmt.Sprintf("%.2f", result.Bandwidth/1024/1024),
			strconv.FormatInt(result.TTFB.Milliseconds(), 10),
		}
		err = csvWriter.Write(line)
		if err != nil {
			return err
		}
	}
	csvWriter.Flush()
	return nil
}
