#!/bin/bash

_name=$1

if [ -z "$1" ]; then
  _name=default
fi

#echo $_name
pod_name=nats-box-${_name}

if [[ `kubectl get po | grep ${pod_name} | grep Running | wc -l` -eq 0 ]]; then
  kubectl delete po ${pod_name} --force --grace-period=0 --ignore-not-found
  kubectl run ${pod_name} --image=docker.intuit.com/oss-analytics/dataflow/service/nats-box:0.14.2 --restart=Never --command -- sleep infinite
  #kubectl run ${pod_name} --image=natsio/nats-box --restart=Never --command -- sleep infinite
  sleep 3
fi

tmp_path="$(mktemp -d)"

#echo ${tmp_path}
trap 'rm -rf ${tmp_path}' EXIT

js_file=${tmp_path}/js
nats_file=${tmp_path}/njs
nats_top_file=${tmp_path}/njs-top
remediate_file=${tmp_path}/remediate

encrypted_str=`kubectl get secret isbsvc-${_name}-js-server -o json | jq '.data.auth' | awk -F\" '{print $2}'`

echo $encrypted_str | base64 -D | grep "\"user\"" | awk -F\" -v _name=$_name '{print "nats -s "$4":"$8"@isbsvc-"_name"-js-svc"}' | grep -v "sys:" | awk '{print $0" $1 $2 $3 $4 $5 $6 $7 $8 $9"}' > ${js_file}
echo $encrypted_str | base64 -D | grep "\"user\"" | awk -F\" -v _name=$_name '{print "nats -s "$4":"$8"@isbsvc-"_name"-js-svc"}' | grep "sys:" | awk '{print $0" $1 $2 $3 $4 $5 $6 $7 $8 $9"}' > ${nats_file}
echo $encrypted_str | base64 -D | grep "\"user\"" | awk -F\" -v _name=$_name '{print "nats-top -s "$4":"$8"@isbsvc-"_name"-js-svc"}' | grep -v "sys:" | awk '{print $0" $1 $2 $3 $4 $5 $6 $7 $8 $9"}' > ${nats_top_file}

cat > $remediate_file <<'_EOF'
#!/bin/sh
n=`njs server report jetstream | grep isbsvc | grep -E 'true|false' | sed 's/yes/ /g' | awk '{print $9}' | grep false | wc -l`
if [[ $n -eq 0 ]]; then
  echo "It looks like all the nodes are good."
  exit 0
elif [[ $n -gt 1 ]]; then
  echo "It looks like there are $n nodes are in bad state, it is hard to do auto remediation, please contact #devx-numaproj-support for help."
  exit 1
fi
total_nodes=`njs server list | grep isbsvc | wc -l`
if [[ $total_nodes -lt 4 ]]; then
  echo "To do remediation, it requires the JetStream cluster has at least 4 nodes. This JetStream cluster only has $total_nodes nodes."
  exit 1
fi
bad_node=`njs server report jetstream | grep isbsvc | grep -E 'true|false' | sed 's/yes/ /g' | grep false | awk '{print $2}' | head -1`
read -p "Are you sure $bad_node has a problem? (y/n): " confirm && [[ $confirm == [yY] || $confirm == [yY][eE][sS] ]] || exit 1
echo
echo "Confirmed!"
echo
echo "Starting remediation process for node $bad_node ..."
echo
js s report | tr ',' ' ' | grep "${bad_node} " | grep -E 'OT|PROC' | awk -v bad_node="$bad_node" '{print "echo; echo \"Fixing stream "$2" ...\"; js s cluster peer-remove "$2" "bad_node"; sleep 5;"}' | sh
js s report | tr ',' ' ' | grep "${bad_node} " | awk -v bad_node="$bad_node" '{print "echo; echo \"Fixing stream "$2" ...\"; js s cluster peer-remove "$2" "bad_node"; sleep 10;"}' | sh
if [[ `js s report | tr ',' ' ' | grep "${bad_node} " | wc -l` -gt 0 ]]; then
  echo "Remediation failed, please re-run the command, if it still fails, please contact #devx-numaproj-support for help."
  exit 1
fi
echo
echo "Successfully removed data copies from the bad node $bad_node."
echo
echo
echo "Please type \"exit\" to exit current container, and run following commands to clean up the pod and pvc for the bad node."
echo
echo "kubectl -n <namespace> delete po ${bad_node}; kubectl -n <namespace> delete pvc isbsvc-default-js-vol-${bad_node}"
echo
_EOF

chmod u+x ${js_file}
chmod u+x ${nats_file}
chmod u+x ${nats_top_file}
chmod u+x ${remediate_file}

kubectl cp ${js_file} ${pod_name}:/usr/local/bin/
kubectl cp ${nats_file} ${pod_name}:/usr/local/bin/
kubectl cp ${nats_top_file} ${pod_name}:/usr/local/bin/
kubectl cp ${remediate_file} ${pod_name}:/usr/local/bin/
kubectl exec -it ${pod_name} -- sh