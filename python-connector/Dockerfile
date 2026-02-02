FROM python:3.12-slim

WORKDIR /app

# Install dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy source code
COPY . .

# Ensure namespaced packages are in path
ENV PYTHONPATH=/app

# Expose gRPC port
EXPOSE 50051

ENTRYPOINT ["python", "main.py"]
