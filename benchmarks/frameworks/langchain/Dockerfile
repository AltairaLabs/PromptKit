FROM python:3.12-slim
WORKDIR /app
COPY benchmarks/frameworks/langchain/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY benchmarks/frameworks/langchain/app.py .
ENV PORT=8091
EXPOSE 8091
CMD ["python", "app.py"]
