FROM python:3.12-slim

WORKDIR /app

ENV TZ=Europe/Zurich
ENV UVICORN_HOST="0.0.0.0"
ENV UVICORN_PORT="8080"

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app

CMD ["uvicorn", "app.main:app", "--log-config", "/app/app/logging.yml"]