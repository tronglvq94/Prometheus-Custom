package main

import (
	"PrometheusCustom/model"
	"PrometheusCustom/util"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Metric struct {
	Cluster   string `json:"cluster"`
	Instance  string `json:"instance"`
	Job       string `json:"job"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
}

type Result struct {
	Metric Metric        `json:"metric"`
	Value  []interface{} `json:"value"`
}

type Data struct {
	ResultType string   `json:"resultType"`
	Result     []Result `json:"result"`
}

type Response struct {
	Status string `json:"status"`
	Data   Data   `json:"data"`
}

type CpuOversaturionResponse struct {
	CpuOversaturion model.CpuOversaturion `json:"cpuOversaturion"`
	Values          [][]interface{}       `json:"values"`
}
type MetricValue struct {
	Timestamp               int64
	Value                   float64
	CpuOversaturionResponse CpuOversaturionResponse
}

func GetCpuOversaturation() []model.CpuOversaturion {
	cpuOversaturationResponse := make([]CpuOversaturionResponse, 0)
	cluster := GetCluster()
	for k, _ := range cluster {
		bodyJson := GetCpuOversaturionByCluster(k)
		for _, data := range bodyJson.Data.Result {
			cpuOversaturationResponse = append(cpuOversaturationResponse, CpuOversaturionResponse{
				CpuOversaturion: model.CpuOversaturion{
					Pod:     data.Metric.Pod,
					Cluster: k,
					Time:    int64(data.Value[len(data.Value)-1][0].(float64)),
				},
				Values: data.Value,
			})
		}
	}
	cpuOversaturationResponse = CheckBurst(cpuOversaturationResponse)
	result := make([]model.CpuOversaturion, 0)
	for _, k := range cpuOversaturationResponse {
		podStartTimeResponse := GetPodStartTime(k.CpuOversaturion.Pod)
		if len(podStartTimeResponse.Data.Result) > 0 {
			timeStart, _ := strconv.ParseFloat(podStartTimeResponse.Data.Result[0].Value[1].(string), 64)
			if time.Now().Sub(time.Unix(int64(timeStart), 0)).Hours() > 1 {
				result = append(result, k.CpuOversaturion)
			}
		}
	}
	return result
}

func GetCpuOversaturionByCluster(cluster string) ResponseResource {
	method := "POST"
	config, _ := util.LoadConfig()
	url := fmt.Sprintf("http://%s/api/v1/query_range", config.PrometheusUrl)
	step := "1h"
	start := time.Now().Add(-6 * time.Hour).Unix()
	query := fmt.Sprintf("quantile_over_time(0.9,sum(node_namespace_pod_container:container_cpu_usage_seconds_total:sum_rate{cluster=\"%s\"}*on(namespace,pod)group_left(workload, workload_type)mixin_pod_workload{cluster=\"%s\", workload_type=\"deployment\"}) by (pod)[1h]/sum(kube_pod_container_resource_requests{resource=\"cpu\", cluster=\"%s\"}* on(namespace,pod)group_left(workload, workload_type) mixin_pod_workload{cluster=\"%s\",workload_type=\"deployment\"}) by (pod)[1h])[6h:1h] > 2", cluster, cluster, cluster, cluster)
	payload := strings.NewReader(fmt.Sprintf("query=%s&start=%d&step=%s", query, start, step))

	client := &http.Client{}
	req, err := http.NewRequest(method, url, payload)

	if err != nil {
		fmt.Println(err)
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	res, _ := client.Do(req)

	defer res.Body.Close()

	body, _ := ioutil.ReadAll(res.Body)
	var bodyJson ResponseResource
	_ = json.Unmarshal(body, &bodyJson)
	return bodyJson
}

func GetPodStartTime(pod string) Response {
	method := "POST"
	config, _ := util.LoadConfig()
	url := fmt.Sprintf("http://%s/api/v1/query", config.PrometheusUrl)
	query := fmt.Sprintf("kube_pod_start_time{pod=\"%s\"}", pod)
	payload := strings.NewReader(fmt.Sprintf("query=%s", query))

	client := &http.Client{}
	req, err := http.NewRequest(method, url, payload)

	if err != nil {
		fmt.Println(err)
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	res, _ := client.Do(req)

	defer res.Body.Close()

	body, _ := ioutil.ReadAll(res.Body)
	var bodyJson Response
	_ = json.Unmarshal(body, &bodyJson)
	return bodyJson
}

func CheckBurst(cpuOversaturions []CpuOversaturionResponse) []CpuOversaturionResponse {
	result := make([]CpuOversaturionResponse, 0)
	for _, k := range cpuOversaturions {
		valueArray := make([]MetricValue, 0)
		for _, value := range k.Values {
			valueArray = append(valueArray, MetricValue{
				Timestamp:               int64(value[0].(float64)),
				Value:                   value[1].(float64),
				CpuOversaturionResponse: k,
			})
		}
		if checkBurstByPod(valueArray) {
			result = append(result, k)
		}
	}
	return result
}

func checkBurstByPod(value []MetricValue) bool {
	ascCount := 0
	descCount := 0
	flag := 0
	for i, k := range value {
		if i == 0 {
			if k.Value > 1 {
				ascCount += 1
				flag = 1
			} else {
				descCount += 1
				flag = 0
			}
		} else {
			if flag == 0 && k.Value > 1 {
				ascCount += 1
				flag = 1
			} else if flag == 1 && k.Value < 1 {
				descCount += 1
				flag = 0
			}
		}
	}
	if math.Abs(float64(ascCount-descCount)) <= 2 {
		return true
	}
	return false
}
