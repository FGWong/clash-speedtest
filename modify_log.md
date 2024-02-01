
 bin.v1.3  release v1.3 下载二进制文件， output参数不支持yaml文件格式，为输出文件名

 master 分支修改
 增加节点带宽阈值，低于该值，不输出到output文件，并且可重命名 output文件
 编译:
 go build -gcflags -S main.go 或者 go tool compile -N -l -S main.go
 mv main main.plus
ln -s /projects/vpn/clash-speedtest/main.plus /usr/local/sbin/clash_speedtest

clash_speedtest -c '/data/www/static/clash/proxies.yaml'  --size 102400 -concurrent 2 -output yaml -outfile "./result/proxies2" -widthred 0.0000001


 v0.0.1
初始版本，可以检验速度并保存结果的代码

v0.1.0
  commit 5b4e1a24fe5dc02aaa408b32218f2d93e568bfdb
 检查 yaml文件含有非法字符，并替换掉: (&quot; 、&quot、 ?)

v0.2.0
#一些节点格式不兼容
#Failed to convert : proxy 24: ss 216.52.183.243:1001 obfs mode error:
obfs 混淆模式已经被废弃，不为clash兼容，即使在配置文件中，也不能夹在，所以要滤除
 如：
  - {name: 🇺🇸 _US_美国 5, server: 216.52.183.243, port: 1001, client-fingerprint: chrome, type: ss, cipher: aes-128-gcm, password: 83XvX4Vo%*3a, tfo: false, plugin: v2ray-plugin, plugin-opts: {mode: "", host: "", path: "", tls: false, mux: false, skip-cert-verify: false}}

注意：trojan型的没有cipher项，无需检查
修改
屏蔽掉 cipher: aes-128-gcm 类型的,但 Clash 源文件中 没有暴露 Cipher 接口，只能仍然从原始配置检测, 但代码整体加载，所以需要抛弃整体。不如不处理
改为 判断 config map结构中的cipher字段值

v0.2.1
vmess 类型需要检查uuid 存在，且长度为36
检查 yaml文件含有非法字符，并替换掉 (*)

v0.2.2
存在port的值等为非法字符，导致单个config解析失败，
或config那么重复，导致添加失败，不再整体退出，continue下一个

v0.2.3
 修正 v0.2.0 对obfs 模式的处理，Shadowsock 插件 obfs 和v2ray-plugin 应该被 clash兼容，但speed-test不兼容

