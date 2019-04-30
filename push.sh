docker build -f Dockerfile -t gcr.io/$PROJECT_ID/chatbot:latest .
gcloud docker -- push gcr.io/$PROJECT_ID/chatbot:latest
kubectl apply -f ./deploy/chatbot.yaml -n infra
kubectl delete pods -l app=chatbot -n infra
