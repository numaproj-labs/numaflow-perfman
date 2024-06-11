#!/bin/bash

NAMESPACE="default"
PIPELINE="perfman-base-pipeline"
VERTICES=("p1" "input" "output")

END_TIME=$(date +%s)
START_TIME=$((END_TIME - 5 * 60))
DURATION=$(((END_TIME - START_TIME) / 60))

STEP=15
RATE_INTERVAL=$((4 * STEP))

if (( RATE_INTERVAL >= 60 )); then
  RATE_INTERVAL=$((RATE_INTERVAL / 60))
  RATE_INTERVAL+="m"
else
  RATE_INTERVAL+="s"
fi

mkdir -p test

RESULT_FILE="test/report-5minutes"
truncate -s 0 $RESULT_FILE

echo "INBOUND MESSAGES (TPS) METRICS OVER $DURATION minutes" >> $RESULT_FILE
printf '%.0s-' {1..45} >> $RESULT_FILE
echo >> $RESULT_FILE
printf "%-15s %s\n" "Vertex" "Average no. of messages (per second)" >> $RESULT_FILE

for vertex in "${VERTICES[@]}"
do
  QUERY="rate(forwarder_read_total{namespace=\"${NAMESPACE}\", pipeline=\"${PIPELINE}\", vertex='${vertex}' }[${RATE_INTERVAL}])"
  PROMETHEUS_URL=http://localhost:9090
  QUERY_ENCODED=$(jq -rn --arg query "$QUERY" '$query|@uri')
  API_ENDPOINT="${PROMETHEUS_URL}/api/v1/query_range?query=${QUERY_ENCODED}&start=${START_TIME}&end=${END_TIME}&step=${STEP}"
  FILENAME="raw-data-5minutes-tps-${vertex}.json"
  curl -s "$API_ENDPOINT" | jq . > "test/${FILENAME}"

  TOTAL=0
  COUNT=0
  for value in $(jq -r '.data.result[0].values[][1]' "test/${FILENAME}"); do
    TOTAL=$(echo "$TOTAL + $value" | bc -l)
    COUNT=$((COUNT + 1))
  done
  AVERAGE=$(echo "scale=4; $TOTAL/$COUNT" | bc -l)
  printf "%-15s %.4f\n" "$vertex" "$AVERAGE" >> $RESULT_FILE
done

echo >> $RESULT_FILE

echo "FORWARDER E2E LATENCY METRICS (P90) OVER $DURATION minutes" >> $RESULT_FILE
printf '%.0s-' {1..50} >> $RESULT_FILE
echo >> $RESULT_FILE
printf "%-15s %s\n" "VERTEX" "Average batch processing time (seconds)" >> $RESULT_FILE

for vertex in "${VERTICES[@]}"
do
  QUERY="histogram_quantile(0.9, rate(forwarder_forward_chunk_processing_time_bucket{namespace=\"${NAMESPACE}\", pipeline=\"${PIPELINE}\", vertex='${vertex}'}[${RATE_INTERVAL}])) / 1000000"
  PROMETHEUS_URL=http://localhost:9090
  QUERY_ENCODED=$(jq -rn --arg query "$QUERY" '$query|@uri')
  API_ENDPOINT="${PROMETHEUS_URL}/api/v1/query_range?query=${QUERY_ENCODED}&start=${START_TIME}&end=${END_TIME}&step=${STEP}"
  FILENAME="raw-data-5minutes-latency-${vertex}.json"
  curl -s "$API_ENDPOINT" | jq . > "test/${FILENAME}"

  TOTAL=0
  COUNT=0
  for value in $(jq -r '.data.result[0].values[][1]' "test/${FILENAME}"); do
    TOTAL=$(echo "$TOTAL + $value" | bc -l)
    COUNT=$((COUNT + 1))
  done
  AVERAGE=$(echo "scale=4; $TOTAL/$COUNT" | bc -l)
  printf "%-15s %.4f\n" "$vertex" "$AVERAGE" >> $RESULT_FILE
done
