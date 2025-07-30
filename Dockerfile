FROM python:3.13-slim

# Set working directory
WORKDIR /app

# Set environment variables
ENV PYTHONUNBUFFERED=1
ENV PYTHONDONTWRITEBYTECODE=1

# Install system dependencies
RUN apt-get update && apt-get install -y \
    && rm -rf /var/lib/apt/lists/*

# Copy dependency files
COPY pyproject.toml uv.lock* ./

# Install uv for fast Python package management
RUN pip install uv

# Install Python dependencies
RUN uv sync --frozen

# Copy application code
COPY . .

# Create jots directory for data persistence
RUN mkdir -p /app/jots

# Expose port
EXPOSE 8000

# Set default environment variables
ENV JOT_HOST=0.0.0.0
ENV JOT_PORT=8000
ENV JOT_DIR=/app/jots

# Create volume for persistent data
VOLUME ["/app/jots"]

# Run the application
CMD ["uv", "run", "main.py"]
