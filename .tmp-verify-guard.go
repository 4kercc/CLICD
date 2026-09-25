package main

import (
	"fmt"
	"os/exec"

	"clicd/internal/config"
	"clicd/internal/kvm"
)

func main() {
	if _, err := config.InitConfig(); err != nil {
		fmt.Println("config init failed:", err)
		return
	}
	fmt.Println("containers loaded:", len(config.AppConfig.Containers))
	for i := range config.AppConfig.Containers {
		c := &config.AppConfig.Containers[i]
		fmt.Printf("  id=%d name=%-16s kvm=%v template=%-14s disk_image=%s\n",
			c.ID, c.Name, c.IsKVM(), c.Template, c.DiskImage)
	}

	fmt.Println()
	fmt.Println("=== raw qemu-img output for vm-4 ===")
	out, err := exec.Command("qemu-img", "info", "-U", "--backing-chain", "--output=json",
		"/var/lib/clicd/kvm/instances/vm-4/disk.qcow2").CombinedOutput()
	fmt.Printf("err=%v\n%s\n", err, string(out))

	fmt.Println("=== kvm.InstancesUsingImage ===")
	for _, id := range []string{"custom-kvm-f58ab36672", "custom-kvm-9a78b2756f"} {
		fmt.Printf("%-24s -> %v\n", id, kvm.InstancesUsingImage(id))
	}
}
