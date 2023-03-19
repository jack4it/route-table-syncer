package main

import (
	"context"
	"flag"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/profiles/latest/network/mgmt/network"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	informerscorev1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const sleepOnError = 5 * time.Second
const minTimeBetweenUpdates = 5 * time.Second

// RouteTableController listens to node changes and syncs node subnet route table to another specified one (e.g., one for app gateway subnet)
type RouteTableController struct {
	informerFactory informers.SharedInformerFactory
	nodeInformer    informerscorev1.NodeInformer
	work            chan *v1.Node
}

// Run starts shared informers, waits for the shared informer cache to synchronize and starts syncer loop
func (c *RouteTableController) Run(stopCh chan struct{}) error {
	// Starts all the shared informers that have been created by the factory so far
	c.informerFactory.Start(stopCh)
	// wait for the initial synchronization of the local cache.
	if !cache.WaitForCacheSync(stopCh, c.nodeInformer.Informer().HasSynced) {
		klog.Exit("informers failed to sync")
	}

	lastUpdate := time.Now().Add(minTimeBetweenUpdates)
	klog.Info("syncer started")
	for {
		select {
		case node := <-c.work:
			klog.Infof("processing node: %s", node.Name)
			since := time.Since(lastUpdate)
			if since < minTimeBetweenUpdates {
				sleep := minTimeBetweenUpdates - since
				klog.V(9).Infof("it has been %+v since last update; sleeping for %+v before next update", since, sleep)
				time.Sleep(sleep)
			}

			if err := reconcile(); err != nil {
				klog.Errorf("error processing node: %v", node.Name, err)
				time.Sleep(sleepOnError)
			} else {
				klog.Infof("processed node: %s", node.Name)
			}

			lastUpdate = time.Now()
		case <-stopCh:
			return nil
		}
	}
}

func (c *RouteTableController) nodeChange(obj interface{}) {
	node := obj.(*v1.Node)
	c.work <- node
}

func (c *RouteTableController) nodeUpdate(_, new interface{}) {
	c.nodeChange(new)
}

// NewRouteTableController creates a RouteTableController
func NewRouteTableController(informerFactory informers.SharedInformerFactory) *RouteTableController {
	nodeInformer := informerFactory.Core().V1().Nodes()

	c := &RouteTableController{
		informerFactory: informerFactory,
		nodeInformer:    nodeInformer,
		work:            make(chan *v1.Node, 64),
	}
	nodeInformer.Informer().AddEventHandler(
		// Your custom resource event handlers.
		cache.ResourceEventHandlerFuncs{
			// Called on creation
			AddFunc: c.nodeChange,
			// Called on resource update and every resyncPeriod on existing resources.
			UpdateFunc: c.nodeUpdate,
			// Called on resource deletion.
			DeleteFunc: c.nodeChange,
		},
	)
	return c
}

var (
	localMode          bool
	azureJsonPath      string
	destRouteTableName string

	cpc *CloudProviderConfig
)

func init() {
	flag.BoolVar(&localMode, "local", true, "set to false for production use")
	flag.StringVar(&azureJsonPath, "azure-json", "/etc/kubernetes/azure.json", "path to azure.json file")
	flag.StringVar(&destRouteTableName, "destination", "rt-agw", "destination route table name")
}

func main() {
	/*

		local test mode
		  kubeconfig
		  az cli creds

		prod mode
		  in cluster kube config
		  SA to read Node data

		  host path mount for azure config
		  MSI to read source route table
		         write dest route table

	*/

	flag.Parse()
	klog.InitFlags(flag.CommandLine)
	defer klog.Flush()

	klog.Infof("args: %+v", os.Args)

	var cfg *rest.Config
	var err error
	if localMode {
		cpc = &CloudProviderConfig{
			SubscriptionID:          "xoxo",
			RouteTableResourceGroup: "MC_rg-xoxo_aks-xoxo_westus2",
			RouteTableName:          "aks-agentpool-37780615-routetable",
			VNetResourceGroup:       "rg-xoxo",
		}

		config := clientcmd.GetConfigFromFileOrDie("/home/jm/.kube/config")
		clientConfig := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{CurrentContext: "aks-xoxo"})
		cfg, err = clientConfig.ClientConfig()
	} else {
		klog.Info("running in production")
		cpc, err = NewCloudProviderConfig(azureJsonPath)
		klog.Infof("cpc: %+v", cpc)
		if err != nil {
			klog.Exit(err)
		}
		cfg, err = rest.InClusterConfig()
	}

	if err != nil {
		klog.Exit(err)
	}

	c := kubernetes.NewForConfigOrDie(cfg)
	factory := informers.NewSharedInformerFactory(c, time.Hour*24)
	controller := NewRouteTableController(factory)
	stop := make(chan struct{})
	defer close(stop)
	err = controller.Run(stop)
	if err != nil {
		klog.Exit(err)
	}
	select {}
}

func reconcile() error {
	src, err := getTable(cpc.RouteTableResourceGroup, cpc.RouteTableName)
	if err != nil {
		return err
	}

	srcMap := genRouteMap(src.Routes)

	dest, err := getTable(cpc.VNetResourceGroup, destRouteTableName)
	if err != nil {
		return err
	}

	destMap := genRouteMap(dest.Routes)

	foundDiff := diff(srcMap, destMap) || diff(destMap, srcMap)
	klog.Infof("found diff? %t", foundDiff)
	if !foundDiff {
		return nil
	}

	client, err := getClient()
	if err != nil {
		return err
	}

	var routes []network.Route
	for _, r := range *src.Routes {
		if strings.HasPrefix(*r.Name, "aks-") {
			routes = append(routes, r)
		}
	}

	dest.Routes = &routes

	klog.Infof("updating route table: %s", *dest.ID)
	update, err := client.CreateOrUpdate(context.Background(), cpc.VNetResourceGroup, *dest.Name, *dest)
	if err != nil {
		return err
	}

	err = update.WaitForCompletionRef(context.Background(), client.Client)
	if err != nil {
		return err
	}

	status := update.Status()
	if status == "Succeeded" {
		klog.Info("updating succeeded")
	} else {
		klog.Warning("updating failed")
	}

	return nil
}

func diff(left map[string]string, right map[string]string) bool {
	var diff bool
	for k, leftVal := range left {
		if rightVal, ok := right[k]; !ok || leftVal != rightVal {
			diff = true
			break
		}
	}
	return diff
}

func genRouteMap(routes *[]network.Route) map[string]string {
	m := map[string]string{}
	for _, r := range *routes {
		//fmt.Println(*r.Name, *r.AddressPrefix, r.NextHopType, *r.NextHopIPAddress)
		if !strings.HasPrefix(*r.Name, "aks-") {
			continue
		}
		var sb strings.Builder
		sb.WriteString(*r.Name)
		sb.WriteString(*r.NextHopIPAddress)
		m[*r.AddressPrefix] = sb.String()
	}
	return m
}

func getTable(group string, name string) (*network.RouteTable, error) {
	client, err := getClient()
	if err != nil {
		return nil, err
	}

	table, err := client.Get(context.Background(), group, name, "")
	if err != nil {
		return nil, err
	}

	return &table, nil
}

func getClient() (*network.RouteTablesClient, error) {
	var authZ autorest.Authorizer
	var err error
	if localMode {
		authZ, err = auth.NewAuthorizerFromCLI()
	} else {
		authZ, err = auth.NewAuthorizerFromEnvironment()
	}

	if err != nil {
		return nil, err
	}

	c := network.NewRouteTablesClient(cpc.SubscriptionID)
	c.Authorizer = authZ
	return &c, nil
}
